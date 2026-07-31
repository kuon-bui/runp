package logstore_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"runp/internal/config"
	"runp/internal/logstore"
)

func TestStoreEvictsOldestAndTagsStreams(t *testing.T) {
	store := logstore.New(t.TempDir(), time.Millisecond)
	t.Cleanup(func() { _ = store.Close() })
	h, err := store.Open("shop", "api", config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(h.Stdout(), "one\ntwo\n")
	_, _ = io.WriteString(h.Stderr(), "three\n")
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot("shop", "api")
	if len(got) != 2 || got[0].Text != "two" || got[1].Stream != logstore.Stderr {
		t.Fatalf("records = %#v", got)
	}
}

func TestQueryFiltersCaseInsensitivelyByStream(t *testing.T) {
	store := logstore.New(t.TempDir(), time.Millisecond)
	t.Cleanup(func() { _ = store.Close() })
	h, err := store.Open("shop", "api", config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(h.Stdout(), "Server READY\n")
	_, _ = io.WriteString(h.Stderr(), "ready warning\n")
	got := store.Query("shop", "api", logstore.Filter{Stream: logstore.Stderr, Query: "READY"})
	if len(got) != 1 || got[0].Text != "ready warning" {
		t.Fatalf("records = %#v", got)
	}
}

func TestClearRemovesBufferedRecordsAndKeepsWriterOpen(t *testing.T) {
	store := logstore.New(t.TempDir(), time.Millisecond)
	t.Cleanup(func() { _ = store.Close() })
	h, err := store.Open("shop", "api", config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(h.Stdout(), "before\n")
	store.Clear("shop", "api")
	if got := store.Snapshot("shop", "api"); len(got) != 0 {
		t.Fatalf("records after clear = %#v", got)
	}
	_, _ = io.WriteString(h.Stdout(), "after\n")
	got := store.Snapshot("shop", "api")
	if len(got) != 1 || got[0].Text != "after" {
		t.Fatalf("records after write = %#v", got)
	}
}

func TestWriterChunksLargeLinesWithoutDataLoss(t *testing.T) {
	store := logstore.New(t.TempDir(), time.Millisecond)
	t.Cleanup(func() { _ = store.Close() })
	h, err := store.Open("shop", "api", config.LogConfig{MaxSizeMB: 3, MaxFiles: 2, BufferLines: 10})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("x", (1<<20)+10)
	if _, err := io.WriteString(h.Stdout(), input); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot("shop", "api")
	if len(got) != 2 || got[0].Text+got[1].Text != input {
		t.Fatalf("record lengths = %d", len(got))
	}
}

func TestEventsAreBatched(t *testing.T) {
	store := logstore.New(t.TempDir(), 5*time.Millisecond)
	t.Cleanup(func() { _ = store.Close() })
	h, err := store.Open("shop", "api", config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(h.Stdout(), "one\ntwo\n")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case event := <-store.Events():
		if event.Project != "shop" || event.Process != "api" || len(event.Records) != 2 {
			t.Fatalf("event = %#v", event)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
