package tui

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/process"
)

func TestProjectFormValidatesAndBuildsCopy(t *testing.T) {
	original := config.Default()
	form, err := newProjectForm(original, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.set("Name", "shop")
	form.set("Directory", t.TempDir()+"/missing")
	if _, err := form.config(); err == nil || !strings.Contains(err.Error(), "projects[0].directory") {
		t.Fatalf("error = %v", err)
	}
	dir := t.TempDir()
	form.set("Directory", dir)
	form.toggle("Autostart")
	got, err := form.config()
	if err != nil {
		t.Fatal(err)
	}
	if len(original.Projects) != 0 {
		t.Fatal("live config mutated")
	}
	if len(got.Projects) != 1 || got.Projects[0].Name != "shop" || got.Projects[0].Directory != dir || !got.Projects[0].Autostart {
		t.Fatalf("config = %#v", got)
	}
}

func TestProjectFormSaveAndEscape(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	saved := 0
	model := New(Services{
		Snapshots: func() controller.Snapshot { return controller.Snapshot{} },
		Config:    func() config.Config { return cfg },
		SaveConfig: func(got config.Config) error {
			saved++
			cfg = got
			return nil
		},
	})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a'})
	updated, _ = updated.(Model).Update(tea.KeyPressMsg{Code: 'p'})
	withForm := updated.(Model)
	if withForm.form == nil {
		t.Fatal("project form missing")
	}
	withForm.form.set("Name", "shop")
	withForm.form.set("Directory", dir)
	updated, cmd := withForm.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("save command missing")
	}
	finished, _ := updated.(Model).Update(cmd())
	if saved != 1 || finished.(Model).form != nil {
		t.Fatalf("saved/form = %d/%v", saved, finished.(Model).form)
	}

	updated, _ = finished.(Model).Update(tea.KeyPressMsg{Code: 'a'})
	updated, _ = updated.(Model).Update(tea.KeyPressMsg{Code: 'p'})
	withForm = updated.(Model)
	withForm.form.set("Name", "discarded")
	updated, _ = withForm.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if updated.(Model).form != nil || len(cfg.Projects) != 1 {
		t.Fatal("escape mutated config")
	}
}

func TestNewProcessSaveDoesNotReadMissingRuntimeSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	model := New(Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop", Directory: dir}}}
		},
		Config:     func() config.Config { return cfg },
		SaveConfig: func(got config.Config) error { cfg = got; return nil },
	})
	opened, _ := model.Update(tea.KeyPressMsg{Code: 'a'})
	opened, _ = opened.(Model).Update(tea.KeyPressMsg{Code: 'o'})
	editing := opened.(Model)
	editing.form.set("Name", "api")
	editing.form.set("Command", "server")
	saving, cmd := editing.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("save command missing")
	}
	_, _ = saving.(Model).Update(cmd())
	if len(cfg.Projects[0].Processes) != 1 {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestProcessFormBuildsAllFields(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"Name":               "api",
		"Command":            "server",
		"Args":               `["--port","8080"]`,
		"Directory":          ".",
		"EnvFile":            ".env",
		"DependsOn":          "db, db , worker",
		"HealthType":         "tcp",
		"HealthURL":          "",
		"HealthAddress":      "127.0.0.1:8080",
		"HealthTimeout":      "2s",
		"HealthInterval":     "100ms",
		"RestartPolicy":      "always",
		"RestartMaxAttempts": "7",
		"RestartWindow":      "2m",
		"InitialBackoff":     "2s",
		"MaxBackoff":         "20s",
		"LogMaxSizeMB":       "20",
		"LogMaxFiles":        "3",
		"LogBufferLines":     "500",
		"StopTimeout":        "4s",
	}
	for label, value := range values {
		form.set(label, value)
	}
	for key, value := range map[string]string{"TOKEN": "secret-value", "PORT": "8080"} {
		form.set("EnvKey", key)
		form.set("EnvValue", value)
		form.setEnvValue()
	}
	form.toggle("Autostart")
	got, err := form.configWithoutValidation()
	if err != nil {
		t.Fatal(err)
	}
	item := got.Projects[0].Processes[0]
	if item.Name != "api" || item.Command != "server" || !reflect.DeepEqual(item.Args, []string{"--port", "8080"}) || item.Directory != "." || item.Env["TOKEN"] != "secret-value" || item.EnvFile != ".env" || !item.Autostart || !reflect.DeepEqual(item.DependsOn, []string{"db", "worker"}) || item.Health.Type != "tcp" || item.Health.Address != "127.0.0.1:8080" || item.Restart.Policy != "always" || item.Restart.MaxAttempts != 7 || item.Log.BufferLines != 500 || time.Duration(item.StopTimeout) != 4*time.Second {
		t.Fatalf("process = %#v", item)
	}
}

func TestProcessFormRejectsShellArgs(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	form, _ := newProcessForm(cfg, 0, -1)
	form.set("Name", "api")
	form.set("Command", "echo hi")
	form.set("Args", `["not-allowed"]`)
	form.toggle("Shell")
	if _, err := form.configWithoutValidation(); err == nil || !strings.Contains(err.Error(), "args must be empty") {
		t.Fatalf("error = %v", err)
	}
}

func TestProcessFormMasksEnvironmentValues(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "server", Env: map[string]string{"TOKEN": "secret-value"}, EnvFile: "/tmp/do-not-read"}}}}
	form, err := newProcessForm(cfg, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	view := form.view()
	if !strings.Contains(view, "TOKEN") || strings.Contains(view, "secret-value") {
		t.Fatalf("unsafe view = %q", view)
	}
	if _, err := os.Stat("/tmp/do-not-read"); err == nil {
		t.Fatal("test envFile unexpectedly exists")
	}
	valueIndex := -1
	for index, field := range form.fields {
		if field.label == "EnvValue" {
			valueIndex = index
		}
	}
	if valueIndex < 0 {
		t.Fatal("password env value input missing")
	}
	form.set("EnvKey", "TOKEN")
	form.set("EnvValue", "replacement-secret")
	form.focus = valueIndex
	if strings.Contains(form.view(), "replacement-secret") {
		t.Fatalf("replacement exposed = %q", form.view())
	}
	form.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got, err := form.configWithoutValidation()
	if err != nil {
		t.Fatal(err)
	}
	if got.Projects[0].Processes[0].Env["TOKEN"] != "replacement-secret" {
		t.Fatalf("env = %#v", got.Projects[0].Processes[0].Env)
	}
	form.focus = valueIndex - 1
	form.update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	got, _ = form.configWithoutValidation()
	if _, exists := got.Projects[0].Processes[0].Env["TOKEN"]; exists {
		t.Fatalf("env key not deleted: %#v", got.Projects[0].Processes[0].Env)
	}
}

func TestFormKeyboardTogglesFocusableBooleans(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	for form.focusLabel() != "Shell" {
		form.update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	form.update(tea.KeyPressMsg{Code: ' '})
	if !form.booleans["Shell"] || !strings.Contains(form.view(), "› Shell: true") {
		t.Fatalf("form = %q", form.view())
	}
	form.update(tea.KeyPressMsg{Code: tea.KeyTab})
	if form.focusLabel() != "Autostart" {
		t.Fatalf("focus = %q", form.focusLabel())
	}
}

func TestActiveProcessCriticalEditRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	cfg := formConfig(dir)
	calls := []string{}
	model := New(formServices(&cfg, process.Running, &calls, nil))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e'})
	editing := updated.(Model)
	editing.form.set("Command", "changed")
	updated, cmd := editing.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd != nil || updated.(Model).pending != saveCritical {
		t.Fatalf("cmd/pending = %v/%v", cmd, updated.(Model).pending)
	}
	confirmed, cmd := updated.(Model).Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Fatal("critical save command missing")
	}
	_, _ = confirmed.(Model).Update(cmd())
	if !reflect.DeepEqual(calls, []string{"stop", "save", "start"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestActiveProcessNeutralEditSavesWithoutStop(t *testing.T) {
	dir := t.TempDir()
	cfg := formConfig(dir)
	calls := []string{}
	model := New(formServices(&cfg, process.Running, &calls, nil))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e'})
	editing := updated.(Model)
	editing.form.toggle("Autostart")
	_, cmd := editing.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("neutral save command missing")
	}
	_ = cmd()
	if !reflect.DeepEqual(calls, []string{"save"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestCriticalEditFailedStopPreventsSave(t *testing.T) {
	dir := t.TempDir()
	cfg := formConfig(dir)
	calls := []string{}
	stopErr := errors.New("stop failed")
	model := New(formServices(&cfg, process.Running, &calls, stopErr))
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'e'})
	editing := updated.(Model)
	editing.form.set("Command", "changed")
	updated, _ = editing.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	_, cmd := updated.(Model).Update(tea.KeyPressMsg{Code: 'y'})
	message := cmd()
	if !reflect.DeepEqual(calls, []string{"stop"}) {
		t.Fatalf("calls = %#v", calls)
	}
	if done := message.(operationDoneMsg); !errors.Is(done.err, stopErr) {
		t.Fatalf("error = %v", done.err)
	}
}

func TestOperationCommandDoesNotOverrideConfiguredTimeouts(t *testing.T) {
	seen := make(chan context.Context, 1)
	cmd := operationCommand(noAction, func(ctx context.Context) error {
		seen <- ctx
		return nil
	})
	_ = cmd()
	ctx := <-seen
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Fatal("UI imposed timeout on lifecycle operation")
	}
}

func formConfig(dir string) config.Config {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "server"}}}}
	return cfg
}

func formServices(cfg *config.Config, state process.State, calls *[]string, stopErr error) Services {
	return Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop", Processes: []controller.ProcessSnapshot{{Name: "api", Runtime: process.Snapshot{State: state}}}}}}
		},
		Config: func() config.Config { return *cfg },
		SaveConfig: func(got config.Config) error {
			*calls = append(*calls, "save")
			*cfg = got
			return nil
		},
		StopProcess: func(context.Context, string, string) error {
			*calls = append(*calls, "stop")
			return stopErr
		},
		StartProcess: func(context.Context, string, string) error {
			*calls = append(*calls, "start")
			return nil
		},
	}
}
