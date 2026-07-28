package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
	"runp/internal/tui"
)

func logModelWithRecords(records []logstore.Record) tui.Model {
	fixture := controller.Snapshot{Projects: []controller.ProjectSnapshot{{
		Name:      "shop",
		Processes: []controller.ProcessSnapshot{{Name: "api", Runtime: process.Snapshot{State: process.Running}}},
	}}}
	return tui.New(tui.Services{
		Snapshots:   func() controller.Snapshot { return fixture },
		LogSnapshot: func(string, string) []logstore.Record { return records },
		LogQuery: func(_, _ string, filter logstore.Filter) []logstore.Record {
			result := make([]logstore.Record, 0)
			for _, record := range records {
				if filter.Stream != "" && record.Stream != filter.Stream {
					continue
				}
				if filter.Query != "" && !strings.Contains(strings.ToLower(record.Text), strings.ToLower(filter.Query)) {
					continue
				}
				result = append(result, record)
			}
			return result
		},
	})
}

func TestLogStreamCycle(t *testing.T) {
	model := logModelWithRecords([]logstore.Record{
		{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "out"},
		{At: time.Unix(2, 0), Stream: logstore.Stderr, Text: "err"},
	})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 't'})
	view := updated.(tui.Model).View().Content
	if !strings.Contains(view, "out") || strings.Contains(view, "err") {
		t.Fatalf("view = %q", view)
	}
	updated, _ = updated.(tui.Model).Update(tea.KeyPressMsg{Code: 't'})
	view = updated.(tui.Model).View().Content
	if strings.Contains(view, "out") || !strings.Contains(view, "err") {
		t.Fatalf("view = %q", view)
	}
}

func TestLogFollow(t *testing.T) {
	model := logModelWithRecords([]logstore.Record{{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "out"}})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(model.View().Content, "FOLLOW") {
		t.Fatal("follow not enabled")
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyUp})
	if !strings.Contains(model.View().Content, "PAUSED") {
		t.Fatal("manual scroll did not pause")
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'f'})
	if !strings.Contains(model.View().Content, "FOLLOW") {
		t.Fatal("follow toggle failed")
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(model.View().Content, "shop") {
		t.Fatal("escape did not return dashboard")
	}
}

func TestCtrlCQuitsFromLogViewer(t *testing.T) {
	model := tui.New(tui.Services{
		Snapshots:   dashboardFixture,
		LogSnapshot: func(string, string) []logstore.Record { return nil },
	})
	opened, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	quitting, cmd := opened.(tui.Model).Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil || !strings.Contains(quitting.(tui.Model).View().Content, "Confirm?") {
		t.Fatal("Ctrl+C ignored in log viewer")
	}
}

func TestLogSearch(t *testing.T) {
	model := logModelWithRecords([]logstore.Record{
		{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "Needle one"},
		{At: time.Unix(2, 0), Stream: logstore.Stdout, Text: "other"},
		{At: time.Unix(3, 0), Stream: logstore.Stderr, Text: "needle two"},
	})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updateModel(t, model, tea.KeyPressMsg{Code: '/'})
	for _, runeValue := range "NEEDLE" {
		model = updateModel(t, model, tea.KeyPressMsg{Code: runeValue, Text: string(runeValue)})
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	view := model.View().Content
	if !strings.Contains(view, "Needle one") || !strings.Contains(view, "needle two") || strings.Contains(view, "other") {
		t.Fatalf("view = %q", view)
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'n'})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'N'})
}

func TestLogHighVolumeBatchesStayResponsive(t *testing.T) {
	records := []logstore.Record{{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "bounded"}}
	events := make(chan logstore.Event, 100)
	model := logModelWithRecords(records)
	services := tui.Services{
		Snapshots: func() controller.Snapshot { return dashboardFixture() },
		LogEvents: events,
		LogSnapshot: func(string, string) []logstore.Record {
			return records
		},
	}
	model = tui.New(services)
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	for range 100 {
		batch := make([]logstore.Record, 100)
		for index := range batch {
			batch[index] = logstore.Record{At: time.Unix(int64(index), 0), Stream: logstore.Stdout, Text: "ignored batch"}
		}
		model, _ = update(model, tuiLogEvent(batch))
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'f'})
	if !strings.Contains(model.View().Content, "PAUSED") || strings.Count(model.View().Content, "bounded") != 1 {
		t.Fatalf("view = %q", model.View().Content)
	}
}

func update(model tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	updated, cmd := model.Update(msg)
	return updated.(tui.Model), cmd
}

func tuiLogEvent(records []logstore.Record) tea.Msg {
	return logstore.Event{Project: "shop", Process: "api", Records: records}
}
