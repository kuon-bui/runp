package tui

import (
	"context"
	"errors"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"runp/internal/config"
	"runp/internal/controller"
	"runp/internal/logstore"
	"runp/internal/process"
)

var formANSIPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func formHasANSICode(value, code string) bool {
	for _, sequence := range formANSIPattern.FindAllString(value, -1) {
		parameters := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b["), "m")
		for _, parameter := range strings.FieldsFunc(parameters, func(r rune) bool { return r == ';' || r == ':' }) {
			if parameter == code {
				return true
			}
		}
	}
	return false
}

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

func TestAddMenuSupportsArrowSelectionAndEnter(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	model := New(Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop"}}}
		},
		Config: func() config.Config { return cfg },
	})

	opened, _ := model.Update(tea.KeyPressMsg{Code: 'a'})
	selected, _ := opened.(Model).Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if selected.(Model).addMenuIndex != 1 {
		t.Fatalf("selection = %d", selected.(Model).addMenuIndex)
	}
	activated, _ := selected.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if activated.(Model).form == nil || activated.(Model).form.kind != processForm {
		t.Fatal("Enter did not open selected process form")
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

func TestModelPropagatesWindowSizeToOpenForm(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	model := New(Services{
		Snapshots: func() controller.Snapshot {
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{Name: "shop"}}}
		},
		Config: func() config.Config { return cfg },
	})
	opened, _ := model.Update(tea.KeyPressMsg{Code: 'a'})
	opened, _ = opened.(Model).Update(tea.KeyPressMsg{Code: 'o'})
	resized, _ := opened.(Model).Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	got := resized.(Model)
	if got.form.width != 70 || got.form.height != 20 {
		t.Fatalf("form size = %dx%d", got.form.width, got.form.height)
	}
	if !strings.Contains(got.View().Content, "[ Basic ]") {
		t.Fatalf("form did not switch to narrow layout: %q", got.View().Content)
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

func TestProcessFormGroupsControlsAndCrossesSectionBoundaryWithKeys(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}

	tests := []struct {
		name        string
		start       string
		key         tea.KeyPressMsg
		wantLabel   string
		wantSection formSection
	}{
		{name: "tab forward", start: "Autostart", key: tea.KeyPressMsg{Code: tea.KeyTab}, wantLabel: "EnvKey", wantSection: environmentSection},
		{name: "down forward", start: "Autostart", key: tea.KeyPressMsg{Code: tea.KeyDown}, wantLabel: "EnvKey", wantSection: environmentSection},
		{name: "shift-tab backward", start: "EnvKey", key: tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, wantLabel: "Autostart", wantSection: basicSection},
		{name: "up backward", start: "EnvKey", key: tea.KeyPressMsg{Code: tea.KeyUp}, wantLabel: "Autostart", wantSection: basicSection},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			form, err := newProcessForm(cfg, 0, -1)
			if err != nil {
				t.Fatal(err)
			}
			for form.focusLabel() != test.start {
				form.update(tea.KeyPressMsg{Code: tea.KeyTab})
			}
			form.update(test.key)
			if form.focusLabel() != test.wantLabel || form.activeSection() != test.wantSection {
				t.Fatalf("focus/section = %q/%v, want %q/%v", form.focusLabel(), form.activeSection(), test.wantLabel, test.wantSection)
			}
		})
	}
}

func TestProcessFormRendersWideSidebarAndOnlyActiveSection(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.resize(100, 30)
	form.toggle("Autostart")
	view := form.view()
	for _, want := range []string{"New process", "▸ Basic", "Command", "Arguments"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %q", want, view)
		}
	}
	for _, unwanted := range []string{"Environment file", "Health type", "Max attempts", "Buffer lines"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("inactive field %q rendered: %q", unwanted, view)
		}
	}
}

func TestProcessFormRendersHorizontalTabsWhenNarrow(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.resize(79, 24)
	view := form.view()
	if !strings.Contains(view, "[ Basic ]") || strings.Contains(view, "▸ Basic\n") {
		t.Fatalf("narrow navigation = %q", view)
	}
}

func TestProcessFormNarrowLongValueUsesNarrowInputViewport(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.set("Command", strings.Repeat("x", 40))
	field := form.fields[form.fieldIndex("Command")]
	rendered := form.renderField(field, 10)
	if got := strings.Count(rendered, "\n") + 1; got != 3 {
		t.Fatalf("rendered field height = %d, want 3: %q", got, rendered)
	}
}

func TestFormRenderedWidthNeverExceedsTinyTerminal(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	for width := 1; width < 9; width++ {
		form.resize(width, 24)
		if got := lipgloss.Width(form.view()); got > width {
			t.Fatalf("width %d rendered as %d cells", width, got)
		}
	}
}

func TestFormTinyFallbackUsesWholeTerminal(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.resize(8, 5)
	if width, height := lipgloss.Size(form.view()); width != 8 || height != 5 {
		t.Fatalf("form = %dx%d, want 8x5", width, height)
	}
}

func TestModalSizeUsesMarginAndTinyFallback(t *testing.T) {
	tests := []struct {
		width, height         int
		wantWidth, wantHeight int
	}{
		{width: 120, height: 30, wantWidth: 90, wantHeight: 26},
		{width: 70, height: 20, wantWidth: 66, wantHeight: 16},
		{width: 59, height: 20, wantWidth: 59, wantHeight: 20},
		{width: 80, height: 15, wantWidth: 80, wantHeight: 15},
	}
	for _, test := range tests {
		width, height := modalSize(test.width, test.height)
		if width != test.wantWidth || height != test.wantHeight {
			t.Fatalf("modal %dx%d = %dx%d", test.width, test.height, width, height)
		}
	}
}

func TestFormModalUsesCalculatedSize(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.resize(100, 24)
	if width, height := lipgloss.Size(form.view()); width != 90 || height != 20 {
		t.Fatalf("form = %dx%d, want 90x20", width, height)
	}
}

func TestFormModalUsesRootBackgroundWithoutRaisedFill(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	form.resize(100, 24)
	view := form.view()
	if formHasANSICode(view, "238") {
		t.Fatalf("raised modal background rendered: %q", view)
	}
	if width, height := lipgloss.Size(view); width != 90 || height != 20 {
		t.Fatalf("form = %dx%d, want 90x20", width, height)
	}
}

func TestFormFocusedFieldHasStrongerBoundaryThanUnfocusedField(t *testing.T) {
	form, err := newProjectForm(config.Default(), -1)
	if err != nil {
		t.Fatal(err)
	}
	name := form.fields[form.fieldIndex(fieldName)]
	directory := form.fields[form.fieldIndex(fieldDirectory)]
	focused := form.renderField(name, 60)
	unfocused := form.renderField(directory, 60)
	if !strings.Contains(focused, "Name") || !strings.Contains(unfocused, "Directory") {
		t.Fatalf("field labels missing: %q / %q", focused, unfocused)
	}
	if !strings.Contains(focused, "━") || strings.Contains(unfocused, "━") {
		t.Fatalf("focus boundary hierarchy missing: %q / %q", focused, unfocused)
	}
	if !formHasANSICode(focused, "96") || !formHasANSICode(unfocused, "90") {
		t.Fatalf("focus palette missing: %q / %q", focused, unfocused)
	}
}

func TestFormViewportKeepsFocusedControlVisible(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, _ := newProcessForm(cfg, 0, -1)
	form.resize(70, 16)
	for form.focusLabel() != toggleAutostart {
		form.moveFocus(1)
	}
	view := form.view()
	if form.body.YOffset() == 0 || !strings.Contains(view, "Autostart") {
		t.Fatalf("focused control clipped: %q", view)
	}
}

func TestProcessFormRendersFriendlyEnumAndToggleControls(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	for form.focusLabel() != "HealthType" {
		form.moveFocus(1)
	}
	form.set("HealthType", "process")
	view := form.view()
	if !strings.Contains(view, "Health type") || !strings.Contains(view, "‹ process ›") {
		t.Fatalf("health controls = %q", view)
	}
}

func TestProcessFormEnumKeysCycleAndWrap(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	tests := []struct {
		label string
		start string
		key   tea.KeyPressMsg
		want  string
	}{
		{label: "HealthType", start: "process", key: tea.KeyPressMsg{Code: tea.KeyLeft}, want: "tcp"},
		{label: "HealthType", start: "tcp", key: tea.KeyPressMsg{Code: tea.KeyRight}, want: "process"},
		{label: "RestartPolicy", start: "never", key: tea.KeyPressMsg{Code: tea.KeyLeft}, want: "always"},
		{label: "RestartPolicy", start: "always", key: tea.KeyPressMsg{Code: tea.KeyRight}, want: "never"},
	}
	for _, test := range tests {
		t.Run(test.label+"/"+test.start, func(t *testing.T) {
			form, err := newProcessForm(cfg, 0, -1)
			if err != nil {
				t.Fatal(err)
			}
			for form.focusLabel() != test.label {
				form.update(tea.KeyPressMsg{Code: tea.KeyTab})
			}
			form.set(test.label, test.start)
			form.update(test.key)
			if got := form.value(test.label); got != test.want {
				t.Fatalf("value = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTextArrowsEditAtCursorWithoutChangingField(t *testing.T) {
	form, err := newProjectForm(config.Default(), -1)
	if err != nil {
		t.Fatal(err)
	}
	form.set(fieldName, "abcd")
	index := form.fieldIndex(fieldName)
	form.fields[index].input.SetCursor(2)
	form.update(tea.KeyPressMsg{Code: tea.KeyLeft})
	form.update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if got := form.value(fieldName); got != "aXbcd" {
		t.Fatalf("value = %q", got)
	}
	if form.focusLabel() != fieldName {
		t.Fatalf("focus = %q", form.focusLabel())
	}
}

func TestTextHomeEndAndDeleteEditAroundCursor(t *testing.T) {
	form, _ := newProjectForm(config.Default(), -1)
	form.set(fieldName, "abcd")
	form.update(tea.KeyPressMsg{Code: tea.KeyHome})
	form.update(tea.KeyPressMsg{Code: tea.KeyDelete})
	form.update(tea.KeyPressMsg{Code: tea.KeyEnd})
	form.update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := form.value(fieldName); got != "bc" {
		t.Fatalf("value = %q", got)
	}
}

func TestUpDownNavigateWhileEnumArrowsCycle(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, _ := newProcessForm(cfg, 0, -1)
	for form.focusLabel() != fieldHealthType {
		form.moveFocus(1)
	}
	form.set(fieldHealthType, config.HealthProcess)
	form.update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := form.value(fieldHealthType); got != config.HealthHTTP {
		t.Fatalf("health type = %q", got)
	}
	form.update(tea.KeyPressMsg{Code: tea.KeyDown})
	if form.focusLabel() != "HealthURL" {
		t.Fatalf("focus = %q", form.focusLabel())
	}
}

func TestFormAttachesParseErrorToField(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, _ := newProcessForm(cfg, 0, -1)
	form.set(fieldArgs, "not-json")
	_, err := form.config()
	if err == nil || form.fieldErrors[fieldArgs] == nil {
		t.Fatalf("error/fields = %v/%#v", err, form.fieldErrors)
	}
	field := form.fields[form.fieldIndex(fieldArgs)]
	if rendered := form.renderField(field, 60); !strings.Contains(rendered, "args:") {
		t.Fatalf("field error hidden: %q", rendered)
	}
}

func TestFormRevealsFieldWithParseError(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, _ := newProcessForm(cfg, 0, -1)
	form.set(fieldArgs, "not-json")
	for form.focusLabel() != fieldHealthType {
		form.moveFocus(1)
	}
	if _, err := form.config(); err == nil {
		t.Fatal("missing parse error")
	}
	if form.focusLabel() != fieldArgs || !strings.Contains(form.view(), "args:") {
		t.Fatalf("parse error hidden on %q: %q", form.focusLabel(), form.view())
	}
}

func TestFormKeepsCrossFieldErrorInSummary(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: t.TempDir()}}
	form, _ := newProcessForm(cfg, 0, -1)
	form.set("Command", "echo hi")
	form.set(fieldArgs, `["bad"]`)
	form.toggle(toggleShell)
	_, err := form.config()
	if err == nil || form.err == nil || len(form.fieldErrors) != 0 {
		t.Fatalf("error/summary/fields = %v/%v/%#v", err, form.err, form.fieldErrors)
	}
}

func TestShiftTabMovesFormFocusBackward(t *testing.T) {
	form, err := newProjectForm(config.Default(), -1)
	if err != nil {
		t.Fatal(err)
	}
	form.update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if form.focusLabel() != "Autostart" {
		t.Fatalf("focus = %q", form.focusLabel())
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
	form.focus = slices.Index(form.focusLabels(), "EnvKey")
	view := form.view()
	if !strings.Contains(view, "TOKEN") || strings.Contains(view, "secret-value") {
		t.Fatalf("unsafe view = %q", view)
	}
	if _, err := os.Stat("/tmp/do-not-read"); err == nil {
		t.Fatal("test envFile unexpectedly exists")
	}
	valueIndex := slices.Index(form.focusLabels(), "EnvValue")
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
	form.focus = slices.Index(form.focusLabels(), "EnvKey")
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
	if !form.booleans["Shell"] || !strings.Contains(form.view(), "[ ON  ] Shell") {
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

func TestProcessRenameRetargetsPreviewAfterSave(t *testing.T) {
	dir := t.TempDir()
	cfg := formConfig(dir)
	selected := ""
	model := New(Services{
		Snapshots: func() controller.Snapshot {
			item := cfg.Projects[0].Processes[0]
			return controller.Snapshot{Projects: []controller.ProjectSnapshot{{
				Name:      cfg.Projects[0].Name,
				Processes: []controller.ProcessSnapshot{{Name: item.Name, Runtime: process.Snapshot{State: process.Stopped}}},
			}}}
		},
		Config: func() config.Config { return cfg },
		SaveConfig: func(got config.Config) error {
			cfg = got
			return nil
		},
		LogSnapshot: func(_, name string) []logstore.Record {
			selected = name
			return nil
		},
	})
	opened, _ := model.Update(tea.KeyPressMsg{Code: 'e'})
	editing := opened.(Model)
	editing.form.set(fieldName, "worker")
	saving, cmd := editing.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("save command missing")
	}
	_, _ = saving.(Model).Update(cmd())
	if selected != "worker" {
		t.Fatalf("preview selection = %q", selected)
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
