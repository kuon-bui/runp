package logstore

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"runp/internal/config"
)

const maxRecordBytes = 1 << 20

const (
	defaultBatchInterval = 50 * time.Millisecond
	storeEventBuffer     = 64
	logDirectoryMode     = 0o700
	bytesPerMegabyte     = 1 << 20
)

type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

type Record struct {
	At     time.Time
	Stream Stream
	Text   string
}

type Event struct {
	Project string
	Process string
	Records []Record
	Err     error
}

type Filter struct {
	Stream Stream
	Query  string
}

type processKey struct {
	project string
	process string
}

type ring struct {
	records []Record
	start   int
	length  int
}

func newRing(capacity int) ring {
	return ring{records: make([]Record, capacity)}
}

func (r *ring) append(record Record) {
	if len(r.records) == 0 {
		return
	}
	if r.length < len(r.records) {
		r.records[(r.start+r.length)%len(r.records)] = record
		r.length++
		return
	}
	r.records[r.start] = record
	r.start = (r.start + 1) % len(r.records)
}

func (r *ring) snapshot() []Record {
	result := make([]Record, r.length)
	for index := range r.length {
		result[index] = r.records[(r.start+index)%len(r.records)]
	}
	return result
}

type entry struct {
	mu     sync.Mutex
	ring   ring
	sink   io.WriteCloser
	closed bool
}

type Store struct {
	root       string
	batchEvery time.Duration

	mu      sync.RWMutex
	entries map[processKey]*entry
	handles map[*Handle]struct{}
	closed  bool

	pendingMu sync.Mutex
	pending   map[processKey]Event
	events    chan Event
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

type Handle struct {
	store   *Store
	key     processKey
	entry   *entry
	stdout  *lineWriter
	stderr  *lineWriter
	closeMu sync.Mutex
	closed  bool
}

func New(root string, batchEvery time.Duration) *Store {
	if batchEvery <= 0 {
		batchEvery = defaultBatchInterval
	}
	store := &Store{
		root:       root,
		batchEvery: batchEvery,
		entries:    make(map[processKey]*entry),
		handles:    make(map[*Handle]struct{}),
		pending:    make(map[processKey]Event),
		events:     make(chan Event, storeEventBuffer),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	go store.batchLoop()
	return store
}

func (s *Store) Events() <-chan Event {
	return s.events
}

func (s *Store) Open(project, process string, cfg config.LogConfig) (*Handle, error) {
	if cfg.BufferLines <= 0 || cfg.MaxSizeMB <= 0 || cfg.MaxFiles <= 0 {
		return nil, fmt.Errorf("log limits must be positive")
	}
	key := processKey{project: project, process: process}
	path := filepath.Join(s.root, config.SafeName(project), config.SafeName(process)+".log")
	if err := os.MkdirAll(filepath.Dir(path), logDirectoryMode); err != nil {
		return nil, err
	}
	sink, err := newRotatingWriter(path, int64(cfg.MaxSizeMB)*bytesPerMegabyte, cfg.MaxFiles)
	if err != nil {
		return nil, err
	}
	current := &entry{ring: newRing(cfg.BufferLines), sink: sink}
	handle := &Handle{store: s, key: key, entry: current}
	handle.stdout = &lineWriter{handle: handle, stream: Stdout}
	handle.stderr = &lineWriter{handle: handle, stream: Stderr}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = sink.Close()
		return nil, fmt.Errorf("log store is closed")
	}
	if previous := s.entries[key]; previous != nil && !previous.closed {
		_ = sink.Close()
		return nil, fmt.Errorf("log %s/%s is already open", project, process)
	}
	s.entries[key] = current
	s.handles[handle] = struct{}{}
	return handle, nil
}

func (s *Store) Snapshot(project, process string) []Record {
	key := processKey{project: project, process: process}
	s.mu.RLock()
	current := s.entries[key]
	s.mu.RUnlock()
	if current == nil {
		return nil
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.ring.snapshot()
}

func (s *Store) Query(project, process string, filter Filter) []Record {
	records := s.Snapshot(project, process)
	result := make([]Record, 0, len(records))
	for _, record := range records {
		if matches(record, filter) {
			result = append(result, record)
		}
	}
	return result
}

func (s *Store) Clear(project, process string) {
	key := processKey{project: project, process: process}
	s.mu.RLock()
	current := s.entries[key]
	s.mu.RUnlock()
	if current == nil {
		return
	}
	current.mu.Lock()
	current.ring = newRing(len(current.ring.records))
	current.mu.Unlock()
	s.queue(key, nil, nil)
}

func matches(record Record, filter Filter) bool {
	if filter.Stream != "" && record.Stream != filter.Stream {
		return false
	}
	return filter.Query == "" || strings.Contains(strings.ToLower(record.Text), strings.ToLower(filter.Query))
}

func (s *Store) append(key processKey, current *entry, stream Stream, text string) {
	record := Record{At: time.Now(), Stream: stream, Text: text}
	var warning error
	current.mu.Lock()
	if current.closed {
		current.mu.Unlock()
		return
	}
	current.ring.append(record)
	if current.sink != nil {
		_, warning = fmt.Fprintf(current.sink, "%s %s %s\n", record.At.Format(time.RFC3339Nano), strings.ToUpper(string(stream)), text)
	}
	current.mu.Unlock()
	s.queue(key, []Record{record}, warning)
}

func (s *Store) queue(key processKey, records []Record, warning error) {
	s.pendingMu.Lock()
	pending := s.pending[key]
	pending.Project = key.project
	pending.Process = key.process
	pending.Records = append(pending.Records, records...)
	if warning != nil {
		pending.Err = warning
	}
	s.pending[key] = pending
	s.pendingMu.Unlock()
}

func (s *Store) batchLoop() {
	defer close(s.done)
	ticker := time.NewTicker(s.batchEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.flush(false)
		case <-s.stop:
			s.flush(true)
			close(s.events)
			return
		}
	}
}

func (s *Store) flush(final bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for key, event := range s.pending {
		if final {
			select {
			case s.events <- event:
			default:
			}
			delete(s.pending, key)
			continue
		}
		select {
		case s.events <- event:
			delete(s.pending, key)
		default:
			return
		}
	}
}

func (s *Store) Close() error {
	var result error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		handles := make([]*Handle, 0, len(s.handles))
		for handle := range s.handles {
			handles = append(handles, handle)
		}
		s.mu.Unlock()
		for _, handle := range handles {
			if err := handle.Close(); err != nil && result == nil {
				result = err
			}
		}
		close(s.stop)
		<-s.done
	})
	return result
}

func (h *Handle) Stdout() io.Writer {
	return h.stdout
}

func (h *Handle) Stderr() io.Writer {
	return h.stderr
}

func (h *Handle) Close() error {
	h.closeMu.Lock()
	defer h.closeMu.Unlock()
	if h.closed {
		return nil
	}
	h.stdout.flush()
	h.stderr.flush()
	h.entry.mu.Lock()
	h.entry.closed = true
	var err error
	if h.entry.sink != nil {
		err = h.entry.sink.Close()
		h.entry.sink = nil
	}
	h.entry.mu.Unlock()
	h.store.mu.Lock()
	delete(h.store.handles, h)
	h.store.mu.Unlock()
	h.closed = true
	return err
}

type lineWriter struct {
	mu      sync.Mutex
	handle  *Handle
	stream  Stream
	partial []byte
}

func (w *lineWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial = append(w.partial, data...)
	w.emit(false)
	return len(data), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.emit(true)
}

func (w *lineWriter) emit(flush bool) {
	for {
		newline := -1
		for index, value := range w.partial {
			if value == '\n' {
				newline = index
				break
			}
		}
		length := newline
		consume := newline + 1
		if newline < 0 {
			if len(w.partial) >= maxRecordBytes {
				length = maxRecordBytes
				consume = maxRecordBytes
			} else if flush && len(w.partial) > 0 {
				length = len(w.partial)
				consume = length
			} else {
				return
			}
		}
		line := w.partial[:length]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		w.handle.store.append(w.handle.key, w.handle.entry, w.stream, string(line))
		w.partial = w.partial[consume:]
	}
}
