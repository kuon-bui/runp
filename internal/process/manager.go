package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"time"

	"runp/internal/config"
	"runp/internal/logstore"
	"runp/internal/procgroup"
)

type Key struct {
	Project string
	Process string
}

type State string

const (
	Stopped    State = "stopped"
	Starting   State = "starting"
	Running    State = "running"
	Stopping   State = "stopping"
	Restarting State = "restarting"
	Failed     State = "failed"
	Blocked    State = "blocked"
)

const (
	managerEventBuffer = 64
	unknownExitCode    = -1
)

type Snapshot struct {
	Key          Key
	State        State
	PID          int
	StartedAt    time.Time
	ExitCode     int
	RestartCount int
	Error        string
}

type Event struct {
	Snapshot Snapshot
}

type runCycle struct {
	ctx      context.Context
	cancel   context.CancelFunc
	exited   chan struct{}
	finished chan struct{}
	finish   sync.Once

	mu      sync.Mutex
	waitErr error
}

type entry struct {
	mu       sync.Mutex
	cfg      config.ResolvedProcess
	snapshot Snapshot
	group    *procgroup.Group
	log      *logstore.Handle
	run      *runCycle
	expected bool
	tracker  RestartTracker
}

type Manager struct {
	logs *logstore.Store

	mu           sync.RWMutex
	entries      map[Key]*entry
	shuttingDown bool

	events       chan Event
	eventMu      sync.Mutex
	eventQueue   []Event
	eventClosing bool
	eventNotify  chan struct{}
	eventDone    chan struct{}
	eventOnce    sync.Once
}

func NewManager(logs *logstore.Store) *Manager {
	manager := &Manager{
		logs:        logs,
		entries:     make(map[Key]*entry),
		events:      make(chan Event, managerEventBuffer),
		eventNotify: make(chan struct{}, 1),
		eventDone:   make(chan struct{}),
	}
	go manager.deliverEvents()
	return manager
}

func (m *Manager) Events() <-chan Event {
	return m.events
}

func (m *Manager) Start(ctx context.Context, cfg config.ResolvedProcess) error {
	key := Key{Project: cfg.ProjectName, Process: cfg.Name}
	m.mu.Lock()
	if m.shuttingDown {
		m.mu.Unlock()
		return fmt.Errorf("process manager is shutting down")
	}
	current := m.entries[key]
	if current == nil {
		current = &entry{snapshot: Snapshot{Key: key, State: Stopped}}
		m.entries[key] = current
	}
	m.mu.Unlock()

	current.mu.Lock()
	switch current.snapshot.State {
	case Starting, Running, Restarting:
		current.mu.Unlock()
		return nil
	case Stopping:
		current.mu.Unlock()
		return fmt.Errorf("%s/%s is stopping", key.Project, key.Process)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cycle := &runCycle{
		ctx:      runCtx,
		cancel:   cancel,
		exited:   make(chan struct{}),
		finished: make(chan struct{}),
	}
	current.cfg = cfg
	current.run = cycle
	current.expected = false
	current.snapshot.State = Starting
	current.snapshot.PID = 0
	current.snapshot.ExitCode = 0
	current.snapshot.Error = ""
	starting := current.snapshot
	current.mu.Unlock()
	m.emit(starting)

	return m.launch(ctx, current, cycle)
}

func (m *Manager) launch(actionCtx context.Context, current *entry, cycle *runCycle) error {
	current.mu.Lock()
	cfg := current.cfg
	current.mu.Unlock()

	handle, err := m.logs.Open(cfg.ProjectName, cfg.Name, cfg.Log)
	if err != nil {
		return m.failStart(current, cycle, err)
	}
	cmd := buildCommand(cfg)
	cmd.Stdout = handle.Stdout()
	cmd.Stderr = handle.Stderr()
	group, err := procgroup.Start(cmd)
	if err != nil {
		_ = handle.Close()
		return m.failStart(current, cycle, err)
	}

	current.mu.Lock()
	if current.run != cycle {
		current.mu.Unlock()
		_ = group.Close()
		_ = handle.Close()
		return fmt.Errorf("process run was replaced")
	}
	current.group = group
	current.log = handle
	current.snapshot.PID = group.PID()
	current.snapshot.StartedAt = time.Now()
	current.mu.Unlock()

	go func() {
		err := group.Wait()
		cycle.mu.Lock()
		cycle.waitErr = err
		cycle.mu.Unlock()
		close(cycle.exited)
	}()

	healthCtx, cancelHealth := context.WithCancel(cycle.ctx)
	stopActionCancel := context.AfterFunc(actionCtx, cancelHealth)
	err = WaitHealthy(healthCtx, cfg.Health, func() bool {
		select {
		case <-cycle.exited:
			return false
		default:
			return true
		}
	})
	stopActionCancel()
	cancelHealth()
	if err != nil {
		select {
		case <-cycle.exited:
		default:
			_ = group.Force()
			<-cycle.exited
		}
		m.finishRun(current, cycle, err)
		return err
	}
	select {
	case <-cycle.exited:
		waitErr := cycle.result()
		m.finishRun(current, cycle, waitErr)
		if waitErr == nil {
			return fmt.Errorf("process exited before becoming healthy")
		}
		return waitErr
	default:
	}

	current.mu.Lock()
	if current.run != cycle || current.expected {
		current.mu.Unlock()
		return fmt.Errorf("process start canceled")
	}
	current.snapshot.State = Running
	running := current.snapshot
	current.mu.Unlock()
	m.emit(running)
	go func() {
		<-cycle.exited
		m.finishRun(current, cycle, cycle.result())
	}()
	return nil
}

func (m *Manager) failStart(current *entry, cycle *runCycle, err error) error {
	current.mu.Lock()
	if current.run == cycle {
		current.snapshot.State = Failed
		current.snapshot.PID = 0
		current.snapshot.ExitCode = unknownExitCode
		current.snapshot.Error = err.Error()
		failed := current.snapshot
		current.mu.Unlock()
		cycle.finish.Do(func() { close(cycle.finished) })
		m.emit(failed)
		return err
	}
	current.mu.Unlock()
	return err
}

func (m *Manager) finishRun(current *entry, cycle *runCycle, cause error) {
	cycle.finish.Do(func() {
		current.mu.Lock()
		if current.run != cycle {
			current.mu.Unlock()
			close(cycle.finished)
			return
		}
		handle := current.log
		group := current.group
		cfg := current.cfg
		expected := current.expected
		current.log = nil
		current.group = nil
		current.snapshot.PID = 0
		current.snapshot.ExitCode = exitCode(cycle.result())
		if expected {
			current.snapshot.State = Stopped
			current.snapshot.Error = ""
		} else {
			current.snapshot.State = Failed
			if cause == nil {
				current.snapshot.Error = "process exited"
			} else {
				current.snapshot.Error = cause.Error()
			}
		}
		result := current.snapshot
		current.mu.Unlock()
		if handle != nil {
			_ = handle.Close()
		}
		if group != nil {
			_ = group.Close()
		}
		if expected || m.isShuttingDown() {
			m.emit(result)
			close(cycle.finished)
			return
		}
		delay, restart := current.tracker.Next(cfg.Restart, cfg.Restart.Policy, false, result.ExitCode, time.Now())
		if !restart {
			m.emit(result)
			close(cycle.finished)
			return
		}
		current.mu.Lock()
		if current.run != cycle || current.expected {
			current.mu.Unlock()
			m.emit(result)
			close(cycle.finished)
			return
		}
		current.snapshot.State = Restarting
		current.snapshot.RestartCount++
		restarting := current.snapshot
		current.mu.Unlock()
		m.emit(restarting)
		go m.restartAfter(current, cycle, delay)
	})
}

func (m *Manager) restartAfter(current *entry, previous *runCycle, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-previous.ctx.Done():
		m.cancelRestart(current, previous)
		return
	}
	if m.isShuttingDown() {
		m.cancelRestart(current, previous)
		return
	}

	runCtx, cancel := context.WithCancel(context.Background())
	next := &runCycle{
		ctx:      runCtx,
		cancel:   cancel,
		exited:   make(chan struct{}),
		finished: make(chan struct{}),
	}
	current.mu.Lock()
	if current.run != previous || current.expected || current.snapshot.State != Restarting {
		current.mu.Unlock()
		cancel()
		close(previous.finished)
		return
	}
	current.run = next
	current.snapshot.State = Starting
	current.snapshot.Error = ""
	starting := current.snapshot
	current.mu.Unlock()
	close(previous.finished)
	m.emit(starting)
	_ = m.launch(context.Background(), current, next)
}

func (m *Manager) cancelRestart(current *entry, cycle *runCycle) {
	current.mu.Lock()
	if current.run == cycle {
		current.snapshot.State = Stopped
		current.snapshot.PID = 0
		current.snapshot.Error = ""
		stopped := current.snapshot
		current.mu.Unlock()
		m.emit(stopped)
		close(cycle.finished)
		return
	}
	current.mu.Unlock()
	close(cycle.finished)
}

func (m *Manager) isShuttingDown() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shuttingDown
}

func (cycle *runCycle) result() error {
	cycle.mu.Lock()
	defer cycle.mu.Unlock()
	return cycle.waitErr
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return unknownExitCode
}

func (m *Manager) Stop(ctx context.Context, key Key) error {
	current, ok := m.entry(key)
	if !ok {
		return nil
	}
	current.mu.Lock()
	if current.snapshot.State == Stopped || current.snapshot.State == Failed || current.snapshot.State == Blocked {
		current.mu.Unlock()
		return nil
	}
	if current.snapshot.State == Stopping {
		cycle := current.run
		group := current.group
		timeout := current.cfg.StopTimeout
		current.mu.Unlock()
		return finishStopping(ctx, cycle, group, timeout)
	}
	current.expected = true
	current.snapshot.State = Stopping
	stopping := current.snapshot
	group := current.group
	cycle := current.run
	timeout := current.cfg.StopTimeout
	if cycle != nil {
		cycle.cancel()
	}
	current.mu.Unlock()
	m.emit(stopping)

	if group == nil {
		return waitFinished(ctx, cycle)
	}
	gracefulErr := group.Graceful()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-cycle.finished:
		return gracefulErr
	case <-timer.C:
		forceErr := group.Force()
		if err := waitFinished(ctx, cycle); err != nil {
			return errors.Join(gracefulErr, forceErr, err)
		}
		return errors.Join(gracefulErr, forceErr)
	case <-ctx.Done():
		return errors.Join(gracefulErr, context.Cause(ctx))
	}
}

func finishStopping(ctx context.Context, cycle *runCycle, group *procgroup.Group, timeout time.Duration) error {
	if cycle == nil {
		return nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-cycle.finished:
		return nil
	case <-timer.C:
		var forceErr error
		if group != nil {
			forceErr = group.Force()
		}
		return errors.Join(forceErr, waitFinished(ctx, cycle))
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func waitFinished(ctx context.Context, cycle *runCycle) error {
	if cycle == nil {
		return nil
	}
	select {
	case <-cycle.finished:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (m *Manager) Restart(ctx context.Context, key Key) error {
	current, ok := m.entry(key)
	if !ok {
		return fmt.Errorf("process %s/%s not found", key.Project, key.Process)
	}
	current.mu.Lock()
	cfg := current.cfg
	current.mu.Unlock()
	if err := m.Stop(ctx, key); err != nil {
		return err
	}
	return m.Start(ctx, cfg)
}

func (m *Manager) Block(ctx context.Context, key Key, reason string) error {
	if err := m.Stop(ctx, key); err != nil {
		return err
	}
	current, ok := m.entry(key)
	if !ok {
		return fmt.Errorf("process %s/%s not found", key.Project, key.Process)
	}
	current.mu.Lock()
	current.snapshot.State = Blocked
	current.snapshot.Error = reason
	blocked := current.snapshot
	current.mu.Unlock()
	m.emit(blocked)
	return nil
}

func (m *Manager) Snapshot(key Key) (Snapshot, bool) {
	current, ok := m.entry(key)
	if !ok {
		return Snapshot{}, false
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.snapshot, true
}

func (m *Manager) Snapshots() []Snapshot {
	m.mu.RLock()
	entries := make([]*entry, 0, len(m.entries))
	for _, current := range m.entries {
		entries = append(entries, current)
	}
	m.mu.RUnlock()
	result := make([]Snapshot, 0, len(entries))
	for _, current := range entries {
		current.mu.Lock()
		result = append(result, current.snapshot)
		current.mu.Unlock()
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key.Project == result[j].Key.Project {
			return result[i].Key.Process < result[j].Key.Process
		}
		return result[i].Key.Project < result[j].Key.Project
	})
	return result
}

func (m *Manager) Shutdown(ctx context.Context) error {
	return m.shutdown(ctx, false)
}

func (m *Manager) ForceShutdown(ctx context.Context) error {
	return m.shutdown(ctx, true)
}

func (m *Manager) shutdown(ctx context.Context, force bool) error {
	m.mu.Lock()
	m.shuttingDown = true
	keys := make([]Key, 0, len(m.entries))
	for key := range m.entries {
		keys = append(keys, key)
	}
	m.mu.Unlock()

	errorsByProcess := make(chan error, len(keys))
	var waits sync.WaitGroup
	for _, key := range keys {
		key := key
		waits.Add(1)
		go func() {
			defer waits.Done()
			if force {
				errorsByProcess <- m.forceStop(ctx, key)
				return
			}
			errorsByProcess <- m.Stop(ctx, key)
		}()
	}
	waits.Wait()
	close(errorsByProcess)
	var result error
	for err := range errorsByProcess {
		result = errors.Join(result, err)
	}
	m.closeEvents()
	return result
}

func (m *Manager) forceStop(ctx context.Context, key Key) error {
	current, ok := m.entry(key)
	if !ok {
		return nil
	}
	current.mu.Lock()
	group := current.group
	cycle := current.run
	if cycle == nil || current.snapshot.State == Stopped || current.snapshot.State == Failed || current.snapshot.State == Blocked {
		current.mu.Unlock()
		return nil
	}
	current.expected = true
	current.snapshot.State = Stopping
	stopping := current.snapshot
	cycle.cancel()
	current.mu.Unlock()
	m.emit(stopping)
	var forceErr error
	if group != nil {
		forceErr = group.Force()
	}
	return errors.Join(forceErr, waitFinished(ctx, cycle))
}

func (m *Manager) entry(key Key) (*entry, bool) {
	m.mu.RLock()
	current, ok := m.entries[key]
	m.mu.RUnlock()
	return current, ok
}

func (m *Manager) emit(snapshot Snapshot) {
	m.eventMu.Lock()
	if !m.eventClosing {
		m.eventQueue = append(m.eventQueue, Event{Snapshot: snapshot})
	}
	m.eventMu.Unlock()
	select {
	case m.eventNotify <- struct{}{}:
	default:
	}
}

func (m *Manager) deliverEvents() {
	defer close(m.eventDone)
	for {
		m.eventMu.Lock()
		if len(m.eventQueue) > 0 {
			event := m.eventQueue[0]
			m.eventQueue = m.eventQueue[1:]
			m.eventMu.Unlock()
			m.events <- event
			continue
		}
		closing := m.eventClosing
		m.eventMu.Unlock()
		if closing {
			close(m.events)
			return
		}
		<-m.eventNotify
	}
}

func (m *Manager) closeEvents() {
	m.eventOnce.Do(func() {
		m.eventMu.Lock()
		m.eventClosing = true
		m.eventMu.Unlock()
		select {
		case m.eventNotify <- struct{}{}:
		default:
		}
		<-m.eventDone
	})
}
