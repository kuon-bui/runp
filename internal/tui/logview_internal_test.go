package tui

import (
	"strings"
	"testing"
	"time"

	"runp/internal/logstore"
)

func TestFormatLogRecordsUsesTimestampAndStreamLabels(t *testing.T) {
	records := []logstore.Record{
		{At: time.Date(2026, 7, 28, 12, 4, 5, 6_000_000, time.Local), Stream: logstore.Stdout, Text: "ready"},
		{At: time.Date(2026, 7, 28, 12, 4, 6, 7_000_000, time.Local), Stream: logstore.Stderr, Text: "failed"},
	}
	got := formatLogRecords(records)
	if len(got) != 2 || !strings.Contains(got[0], "12:04:05.006 OUT ready") || !strings.Contains(got[1], "12:04:06.007 ERR failed") {
		t.Fatalf("lines = %#v", got)
	}
}

func TestLogPreviewShowsTailAndEmptyMessage(t *testing.T) {
	records := []logstore.Record{
		{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "old"},
		{At: time.Unix(2, 0), Stream: logstore.Stdout, Text: "new"},
	}
	preview := newLogPreview(30, 1)
	preview.show("shop", "api", Services{LogSnapshot: func(project, process string) []logstore.Record {
		if project != "shop" || process != "api" {
			t.Fatalf("selection = %s/%s", project, process)
		}
		return records
	}})
	if got := preview.render(); strings.Contains(got, "old") || !strings.Contains(got, "new") {
		t.Fatalf("preview = %q", got)
	}
	preview.show("shop", "web", Services{})
	if got := preview.render(); !strings.Contains(got, "Waiting for output…") {
		t.Fatalf("empty preview = %q", got)
	}
}

func TestLogPreviewMatchesSelectedProcess(t *testing.T) {
	preview := newLogPreview(20, 2)
	preview.show("shop", "api", Services{})
	if !preview.matches(logstore.Event{Project: "shop", Process: "api"}) || preview.matches(logstore.Event{Project: "shop", Process: "web"}) {
		t.Fatal("preview matched wrong process")
	}
}

func TestLogViewerViewportFitsPaneContent(t *testing.T) {
	view := newLogView("shop", "api", 90, 22)
	want := 90 - paneFrameWidth - 2*paneHorizontalPadding
	if got := view.viewport.Width(); got != want {
		t.Fatalf("viewport width = %d, want %d", got, want)
	}
	view.resize(60, 16)
	want = 60 - paneFrameWidth - 2*paneHorizontalPadding
	if got := view.viewport.Width(); got != want {
		t.Fatalf("resized viewport width = %d, want %d", got, want)
	}
}
