package tui_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
	"runp/internal/tui"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

const (
	ansiSurface = "236"
	ansiRaised  = "238"
	ansiAccent  = "96"
	ansiMuted   = "90"
	ansiSuccess = "92"
)

func stripANSI(value string) string { return ansiPattern.ReplaceAllString(value, "") }

func hasANSICode(value, code string) bool {
	for _, sequence := range ansiPattern.FindAllString(value, -1) {
		parameters := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
		for _, parameter := range strings.FieldsFunc(parameters, func(r rune) bool { return r == ';' || r == ':' }) {
			if parameter == code {
				return true
			}
		}
	}
	return false
}

func assertTerminalNativePalette(t *testing.T, view string) {
	t.Helper()
	if hasANSICode(view, ansiRaised) {
		t.Fatalf("raised background rendered: %q", view)
	}
	for _, want := range []string{ansiSurface, ansiAccent, ansiMuted} {
		if !hasANSICode(view, want) {
			t.Fatalf("view missing ANSI code %q: %q", want, view)
		}
	}
}

func dashboardFixture() controller.Snapshot {
	return controller.Snapshot{Projects: []controller.ProjectSnapshot{
		{
			Name: "shop",
			Processes: []controller.ProcessSnapshot{
				{Name: "api", Runtime: process.Snapshot{State: process.Running, PID: 1832}},
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
	plain := stripANSI(view.Content)
	if !view.AltScreen || !strings.Contains(plain, "RUNNING") || !strings.Contains(plain, "STOPPED") || !strings.Contains(plain, "shop") {
		t.Fatalf("view = %#v", view)
	}
}

func TestDashboardUsesWholeTerminalAndShowsPID(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	view := model.View().Content
	if width, height := lipgloss.Size(view); width != 120 || height != 30 {
		t.Fatalf("screen = %dx%d", width, height)
	}
	plain := stripANSI(view)
	for _, want := range []string{"2 PROJECTS", "PROJECTS", "PROCESSES", "NAME", "STATE", "PID", "LIVE LOG", "1832", "—"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("view missing %q: %q", want, plain)
		}
	}
}

func TestDashboardUsesTerminalNativePalette(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	view := model.View().Content
	assertTerminalNativePalette(t, view)
	if !hasANSICode(view, ansiSuccess) || !hasANSICode(view, "24") || !strings.Contains(stripANSI(view), "› web") {
		t.Fatalf("semantic state or selection marker missing: %q", view)
	}
}

func TestDashboardHighlightsSelectedProcess(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 60, Height: 20})
	if view := model.View().Content; !hasANSICode(view, "24") || !strings.Contains(stripANSI(view), "› api") {
		t.Fatalf("selected process not highlighted: %q", view)
	}
}

func TestDashboardNamesSelectedLogPreview(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	if plain := stripANSI(model.View().Content); !strings.Contains(plain, "LIVE LOG · SHOP / API") {
		t.Fatalf("log preview context missing: %q", plain)
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	if plain := stripANSI(model.View().Content); !strings.Contains(plain, "LIVE LOG · SHOP / WEB") {
		t.Fatalf("updated log preview context missing: %q", plain)
	}
}

func TestDashboardBreakpoints(t *testing.T) {
	tests := []struct {
		name, marker string
		width        int
	}{
		{name: "wide", width: 100, marker: "PROJECTS"},
		{name: "medium", width: 99, marker: "SHOP"},
		{name: "narrow", width: 69, marker: "PROCESSES · SHOP"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := tui.New(tui.Services{Snapshots: dashboardFixture})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: test.width, Height: 24})
			plain := stripANSI(model.View().Content)
			if !strings.Contains(plain, test.marker) {
				t.Fatalf("view missing %q: %q", test.marker, plain)
			}
			if test.width < 70 && strings.Index(plain, "LIVE LOG") < strings.Index(plain, "PROCESSES · SHOP") {
				t.Fatalf("narrow panes not vertical: %q", plain)
			}
		})
	}
}

func TestDashboardFooterKeepsAllActionKeysVisible(t *testing.T) {
	tests := []struct {
		width int
		want  []string
	}{
		{width: 60, want: []string{"Enter Log", "s k r c g a e d q"}},
		{width: 120, want: []string{"Enter Log", "s Start", "k Stop", "r Restart", "c Clear log", "g Project", "a Add", "e Edit", "d Delete", "q Quit"}},
	}
	for _, test := range tests {
		model := tui.New(tui.Services{Snapshots: dashboardFixture})
		model = updateModel(t, model, tea.WindowSizeMsg{Width: test.width, Height: 20})
		lines := strings.Split(stripANSI(model.View().Content), "\n")
		footer := lines[len(lines)-1]
		for _, want := range test.want {
			if !strings.Contains(footer, want) {
				t.Fatalf("width %d footer missing %q: %q", test.width, want, footer)
			}
		}
	}
}

func TestKeyboardShortcutsModalShowsAllContextsAndBlocksInput(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updateModel(t, model, tea.KeyPressMsg{Code: '?'})
	plain := stripANSI(model.View().Content)
	for _, want := range []string{
		"KEYBOARD SHORTCUTS", "DASHBOARD", "LOGS & SEARCH", "FORM", "MENUS & CONFIRM",
		"[Enter] Open logs", "[Ctrl+S/Esc] Save / Cancel", "[Ctrl+X] Delete environment",
		"[p] Add project", "[o] Add process", "[y/n/Esc] Confirm / Cancel",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("shortcuts missing %q: %q", want, plain)
		}
	}
	if width, height := lipgloss.Size(model.View().Content); width != 100 || height != 24 {
		t.Fatalf("screen = %dx%d", width, height)
	}
	for _, border := range []string{"┌", "┐", "└", "┘"} {
		if strings.Count(plain, border) < 5 {
			t.Fatalf("section frames missing %q: %q", border, plain)
		}
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(model.View().Content, "› api") {
		t.Fatalf("dashboard moved behind shortcuts: %q", model.View().Content)
	}
}

func TestKeyboardShortcutsModalOpensOverLogViewer(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updateModel(t, model, tea.KeyPressMsg{Code: '?'})
	if !strings.Contains(stripANSI(model.View().Content), "KEYBOARD SHORTCUTS") {
		t.Fatalf("shortcuts missing over logs: %q", model.View().Content)
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: '?'})
	if !strings.Contains(stripANSI(model.View().Content), "LOG OUTPUT") {
		t.Fatalf("log viewer not restored: %q", model.View().Content)
	}
}

func TestDashboardClearsSelectedProcessLogAfterConfirmation(t *testing.T) {
	var clearedProject, clearedProcess string
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		ClearLog: func(project, process string) {
			clearedProject, clearedProcess = project, process
		},
	})
	pending, cmd := model.Update(tea.KeyPressMsg{Code: 'c'})
	if cmd != nil || !strings.Contains(pending.(tui.Model).View().Content, "CONFIRM CLEAR LOG") {
		t.Fatal("clear confirmation missing")
	}
	confirmed, cmd := pending.(tui.Model).Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Fatal("clear command missing")
	}
	_, _ = confirmed.(tui.Model).Update(cmd())
	if clearedProject != "shop" || clearedProcess != "api" {
		t.Fatalf("cleared = %s/%s", clearedProject, clearedProcess)
	}
}

func TestDashboardPreviewTracksSelectionAndEvents(t *testing.T) {
	selected := ""
	refreshes := 0
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		LogSnapshot: func(_, process string) []logstore.Record {
			selected = process
			refreshes++
			return []logstore.Record{{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: process + " output"}}
		},
	})
	if !strings.Contains(model.View().Content, "api output") {
		t.Fatalf("initial preview = %q", model.View().Content)
	}
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	if selected != "web" || !strings.Contains(model.View().Content, "web output") {
		t.Fatalf("selection/preview = %q/%q", selected, model.View().Content)
	}
	beforeEvent := refreshes
	model, _ = update(model, logstore.Event{Project: "shop", Process: "web"})
	if refreshes != beforeEvent+1 {
		t.Fatalf("event refreshes = %d, want %d", refreshes, beforeEvent+1)
	}
}

func TestDashboardKeepsSelectedProjectVisible(t *testing.T) {
	projects := make([]controller.ProjectSnapshot, 20)
	for index := range projects {
		projects[index] = controller.ProjectSnapshot{Name: fmt.Sprintf("project-%02d", index)}
	}
	model := tui.New(tui.Services{Snapshots: func() controller.Snapshot {
		return controller.Snapshot{Projects: projects}
	}})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 10})
	for range 19 {
		model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	plain := stripANSI(model.View().Content)
	if !strings.Contains(plain, "› project-19") || strings.Contains(plain, "project-00") {
		t.Fatalf("project viewport = %q", plain)
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
	if cmd != nil || !strings.Contains(updated.(tui.Model).View().Content, "PROJECT ACTIONS") {
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

func TestDeleteProcessStopsThenSavesAndRemovesDependencies(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		Name:      "shop",
		Directory: dir,
		Processes: []config.Process{
			{Name: "api", Command: "api"},
			{Name: "web", Command: "web", DependsOn: []string{"api"}},
		},
	}}
	calls := []string{}
	model := tui.New(tui.Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{
				Name: "shop",
				Processes: []controller.ProcessSnapshot{
					{Name: "api", Runtime: process.Snapshot{State: process.Running}},
					{Name: "web", DependsOn: []string{"api"}, Runtime: process.Snapshot{State: process.Running}},
				},
			}}}
		},
		Config: func() config.Config { return cfg },
		StopProcess: func(_ context.Context, project, name string) error {
			calls = append(calls, "stop "+project+"/"+name)
			return nil
		},
		SaveConfig: func(got config.Config) error {
			calls = append(calls, "save")
			cfg = got
			return nil
		},
	})
	pending, cmd := model.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd != nil || !strings.Contains(pending.(tui.Model).View().Content, "CONFIRM DELETE PROCESS") {
		t.Fatal("process delete confirmation missing")
	}
	confirmed, cmd := pending.(tui.Model).Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Fatal("process delete command missing")
	}
	_, _ = confirmed.(tui.Model).Update(cmd())
	if strings.Join(calls, ",") != "stop shop/api,save" {
		t.Fatalf("calls = %#v", calls)
	}
	if len(cfg.Projects[0].Processes) != 1 || cfg.Projects[0].Processes[0].Name != "web" || len(cfg.Projects[0].Processes[0].DependsOn) != 0 {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestDeleteProjectStopsThenSaves(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}, {Name: "tools", Directory: dir}}
	calls := []string{}
	model := tui.New(tui.Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop"}, {Name: "tools"}}}
		},
		Config: func() config.Config { return cfg },
		StopProject: func(_ context.Context, name string) error {
			calls = append(calls, "stop "+name)
			return nil
		},
		SaveConfig: func(got config.Config) error {
			calls = append(calls, "save")
			cfg = got
			return nil
		},
	})
	menu, _ := model.Update(tea.KeyPressMsg{Code: 'g'})
	pending, cmd := menu.(tui.Model).Update(tea.KeyPressMsg{Code: 'd'})
	if cmd != nil || !strings.Contains(pending.(tui.Model).View().Content, "CONFIRM DELETE PROJECT") {
		t.Fatal("project delete confirmation missing")
	}
	confirmed, cmd := pending.(tui.Model).Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Fatal("project delete command missing")
	}
	_, _ = confirmed.(tui.Model).Update(cmd())
	if strings.Join(calls, ",") != "stop shop,save" {
		t.Fatalf("calls = %#v", calls)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0].Name != "tools" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestDeleteStopFailurePreventsSave(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "api"}}}}
	saves := 0
	model := tui.New(tui.Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop", Processes: []controller.ProcessSnapshot{{Name: "api"}}}}}
		},
		Config: func() config.Config { return cfg },
		StopProcess: func(context.Context, string, string) error {
			return fmt.Errorf("stop failed")
		},
		SaveConfig: func(config.Config) error { saves++; return nil },
	})
	pending, _ := model.Update(tea.KeyPressMsg{Code: 'd'})
	confirmed, cmd := pending.(tui.Model).Update(tea.KeyPressMsg{Code: 'y'})
	finished, _ := confirmed.(tui.Model).Update(cmd())
	if saves != 0 || !strings.Contains(finished.(tui.Model).View().Content, "stop failed") {
		t.Fatalf("saves/view = %d/%q", saves, finished.(tui.Model).View().Content)
	}
}

func TestProjectMenuRendersCenteredWithoutGrowingScreen(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'g'})
	view := model.View().Content
	if width, height := lipgloss.Size(view); width != 100 || height != 24 {
		t.Fatalf("screen = %dx%d", width, height)
	}
	lines := strings.Split(stripANSI(view), "\n")
	menuLine := -1
	for index, line := range lines {
		if strings.Contains(line, "PROJECT ACTIONS") {
			menuLine = index
			break
		}
	}
	if menuLine < 6 || menuLine > 16 {
		t.Fatalf("menu row = %d", menuLine)
	}
}

func TestProjectMenuUsesCompactTerminalNativePalette(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'g'})
	view := model.View().Content
	assertTerminalNativePalette(t, view)
	if width, height := lipgloss.Size(view); width != 100 || height != 24 {
		t.Fatalf("screen = %dx%d", width, height)
	}
}

func TestOverlayBlocksDashboardNavigation(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'g'})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(model.View().Content, "› api") {
		t.Fatalf("dashboard moved behind menu: %q", model.View().Content)
	}
}

func TestConfirmationUsesHighestVisualLayer(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 24})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'r'})
	plain := stripANSI(model.View().Content)
	if !strings.Contains(plain, "CONFIRM RESTART") || !strings.Contains(plain, "[y] Yes") {
		t.Fatalf("confirmation = %q", plain)
	}
	for line := range strings.SplitSeq(plain, "\n") {
		if before, _, ok := strings.Cut(line, "CONFIRM RESTART"); ok {
			logCenter := (24 + 46 + 120) / 2
			titleCenter := len([]rune(before)) + len([]rune("CONFIRM RESTART"))/2
			if titleCenter < logCenter-3 || titleCenter > logCenter+3 {
				t.Fatalf("confirmation center = %d, want %d: %q", titleCenter, logCenter, line)
			}
			return
		}
	}
	t.Fatal("confirmation title missing")
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

func TestFormRendersAsModalOverDashboard(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	model := tui.New(tui.Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop", Directory: dir}}}
		},
		Config: func() config.Config { return cfg },
	})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'g'})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'e'})
	plain := stripANSI(model.View().Content)
	if !strings.Contains(plain, "RUNP") || !strings.Contains(plain, "Edit project · shop") {
		t.Fatalf("modal/background = %q", plain)
	}
	if width, height := lipgloss.Size(model.View().Content); width != 100 || height != 24 {
		t.Fatalf("screen = %dx%d", width, height)
	}
}

func TestDashboardInputBlockedWhileFormOpen(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "api"}, {Name: "web", Command: "web"}}}}
	model := tui.New(tui.Services{Snapshots: dashboardFixture, Config: func() config.Config { return cfg }})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'e'})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyRight})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(model.View().Content, "› api") {
		t.Fatalf("dashboard moved behind form: %q", model.View().Content)
	}
}

func TestBusySaveBlocksFormInput(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "api"}}}}
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		Config:    func() config.Config { return cfg },
		SaveConfig: func(config.Config) error {
			return nil
		},
	})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'e'})
	saving, cmd := model.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("save command missing")
	}
	blocked, _ := saving.(tui.Model).Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	blocked, _ = blocked.(tui.Model).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	view := blocked.(tui.Model).View().Content
	if !strings.Contains(view, "WORKING") || !strings.Contains(view, "Edit process · api") {
		t.Fatalf("busy layer accepted form input: %q", blocked.(tui.Model).View().Content)
	}
}

func TestStartWorkingModalCentersInLogPane(t *testing.T) {
	model := tui.New(tui.Services{
		Snapshots:    dashboardFixture,
		StartProcess: func(context.Context, string, string) error { return nil },
	})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 24})
	started, cmd := model.Update(tea.KeyPressMsg{Code: 's'})
	if cmd == nil {
		t.Fatal("start command missing")
	}
	for _, line := range strings.Split(stripANSI(started.(tui.Model).View().Content), "\n") {
		if bytePosition := strings.Index(line, "WORKING"); bytePosition >= 0 {
			logCenter := (24 + 46 + 120) / 2
			runes := []rune(line)
			titleStart := len([]rune(line[:bytePosition]))
			left, right := titleStart-1, titleStart
			for left >= 0 && runes[left] != '│' {
				left--
			}
			for right < len(runes) && runes[right] != '│' {
				right++
			}
			modalCenter := (left + right) / 2
			if modalCenter < logCenter-2 || modalCenter > logCenter+2 {
				t.Fatalf("working modal center = %d, want %d: %q", modalCenter, logCenter, line)
			}
			return
		}
	}
	t.Fatal("working modal missing")
}

func TestMatchingLogEventRefreshesPreviewBehindForm(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "api"}}}}
	records := []logstore.Record{{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "before"}}
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		Config:    func() config.Config { return cfg },
		LogSnapshot: func(string, string) []logstore.Record {
			return records
		},
	})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'e'})
	records = []logstore.Record{{At: time.Unix(2, 0), Stream: logstore.Stdout, Text: "after"}}
	model, _ = update(model, logstore.Event{Project: "shop", Process: "api", Records: records})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(model.View().Content, "after") {
		t.Fatalf("preview did not refresh behind form: %q", model.View().Content)
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
	if cmd != nil || !strings.Contains(updated.(tui.Model).View().Content, "CONFIRM SHUTDOWN") {
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
	if cmd == nil || strings.Contains(updated.(tui.Model).View().Content, "CONFIRM SHUTDOWN") {
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
