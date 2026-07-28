package tui_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/process"
	"runp/internal/tui"
)

func dashboardFixture() controller.Snapshot {
	return controller.Snapshot{Projects: []controller.ProjectSnapshot{
		{
			Name: "shop",
			Processes: []controller.ProcessSnapshot{
				{Name: "api", Runtime: process.Snapshot{State: process.Running}},
				{Name: "web", Runtime: process.Snapshot{State: process.Stopped}},
			},
		},
		{
			Name: "tools",
			Processes: []controller.ProcessSnapshot{
				{Name: "worker", Runtime: process.Snapshot{State: process.Stopped}},
			},
		},
	}}
}

func TestDashboardNavigation(t *testing.T) {
	fixture := dashboardFixture()
	model := tui.New(tui.Services{Snapshots: func() controller.Snapshot { return fixture }})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	if !strings.Contains(model.View().Content, "› web") {
		t.Fatalf("view = %q", model.View().Content)
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	if !strings.Contains(model.View().Content, "› worker") {
		t.Fatalf("view = %q", model.View().Content)
	}
	fixture.Projects = fixture.Projects[:1]
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	if !strings.Contains(model.View().Content, "› api") {
		t.Fatalf("selection not clamped: %q", model.View().Content)
	}
}

func TestDashboardResize(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 60, Height: 20})
	view := model.View()
	plain := regexp.MustCompile(`\x1b\[[0-9;:]*m`).ReplaceAllString(view.Content, "")
	if !view.AltScreen || !strings.Contains(plain, "RUNNING") || !strings.Contains(plain, "STOPPED") || strings.Index(plain, "shop") > strings.Index(plain, "tools") {
		t.Fatalf("view = %#v", view)
	}
}

func TestStopRequiresConfirmation(t *testing.T) {
	calls := 0
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		StopProcess: func(context.Context, string, string) error {
			calls++
			return nil
		},
	})
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'k'})
	if cmd != nil {
		t.Fatal("stop ran before confirmation")
	}
	confirmed, cmd := updated.(tui.Model).Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Fatal("missing stop command")
	}
	_ = confirmed
	_ = cmd()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestConfirmationCancelDoesNothing(t *testing.T) {
	calls := 0
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		RestartProcess: func(context.Context, string, string) error {
			calls++
			return nil
		},
	})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'r'})
	_, cmd := updated.(tui.Model).Update(tea.KeyPressMsg{Code: 'n'})
	if cmd != nil || calls != 0 {
		t.Fatalf("cancel command/calls = %v/%d", cmd, calls)
	}
}

func TestProjectActionMenu(t *testing.T) {
	calls := 0
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		StartProject: func(context.Context, string) error {
			calls++
			return nil
		},
	})
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'g'})
	if cmd != nil || !strings.Contains(updated.(tui.Model).View().Content, "Project:") {
		t.Fatal("project menu missing")
	}
	_, cmd = updated.(tui.Model).Update(tea.KeyPressMsg{Code: 's'})
	if cmd == nil {
		t.Fatal("missing project start command")
	}
	_ = cmd()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestProjectEditRoute(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	model := tui.New(tui.Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop", Directory: dir}}}
		},
		Config: func() config.Config { return cfg },
	})
	opened, _ := model.Update(tea.KeyPressMsg{Code: 'g'})
	edited, _ := opened.(tui.Model).Update(tea.KeyPressMsg{Code: 'e'})
	if !strings.Contains(edited.(tui.Model).View().Content, "Edit project · shop") {
		t.Fatalf("view = %q", edited.(tui.Model).View().Content)
	}
}

func TestQuitConfirmsOnlyWithActiveProcesses(t *testing.T) {
	calls := 0
	services := tui.Services{
		Snapshots: dashboardFixture,
		Shutdown: func(context.Context) error {
			calls++
			return nil
		},
	}
	active := tui.New(services)
	updated, cmd := active.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd != nil || !strings.Contains(updated.(tui.Model).View().Content, "Confirm?") {
		t.Fatal("active quit skipped confirmation")
	}

	inactiveFixture := dashboardFixture()
	for projectIndex := range inactiveFixture.Projects {
		for processIndex := range inactiveFixture.Projects[projectIndex].Processes {
			inactiveFixture.Projects[projectIndex].Processes[processIndex].Runtime.State = process.Stopped
		}
	}
	services.Snapshots = func() controller.Snapshot { return inactiveFixture }
	inactive := tui.New(services)
	_, cmd = inactive.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("inactive quit missing shutdown command")
	}
	_ = cmd()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestShutdownRequestSkipsConfirmationAndWaitsForCleanup(t *testing.T) {
	release := make(chan struct{})
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		Shutdown: func(context.Context) error {
			<-release
			return nil
		},
	})
	updated, cmd := model.Update(tui.ShutdownRequestMsg{})
	if cmd == nil || strings.Contains(updated.(tui.Model).View().Content, "Confirm?") {
		t.Fatal("signal shutdown did not start immediately")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-done:
		t.Fatal("shutdown completed before process cleanup")
	default:
	}
	close(release)
	finished, quit := updated.(tui.Model).Update(<-done)
	if quit == nil || finished.(tui.Model).View().Content == "" {
		t.Fatal("shutdown completion did not quit TUI")
	}
}

func TestShutdownRequestPreemptsBusyUI(t *testing.T) {
	calls := 0
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		Shutdown: func(context.Context) error {
			calls++
			return nil
		},
	})
	started, start := model.Update(tea.KeyPressMsg{Code: 's'})
	busy, shutdown := started.(tui.Model).Update(tui.ShutdownRequestMsg{})
	_ = start
	if shutdown == nil {
		t.Fatal("busy UI dropped shutdown request")
	}
	_, _ = busy.(tui.Model).Update(shutdown())
	if calls != 1 {
		t.Fatalf("shutdown calls = %d", calls)
	}
}

func TestOperationErrorRemainsVisible(t *testing.T) {
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		StartProcess: func(context.Context, string, string) error {
			return context.DeadlineExceeded
		},
	})
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 's'})
	if cmd == nil {
		t.Fatal("missing start command")
	}
	finished, _ := updated.(tui.Model).Update(cmd())
	if !strings.Contains(finished.(tui.Model).View().Content, context.DeadlineExceeded.Error()) {
		t.Fatal("operation error hidden")
	}
}

func updateModel(t *testing.T, model tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	updated, _ := model.Update(msg)
	return updated.(tui.Model)
}
