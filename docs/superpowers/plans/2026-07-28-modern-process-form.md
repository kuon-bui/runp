# Modern Process Form Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace flat process form with responsive grouped sidebar/tab editor while preserving config and keyboard behavior.

**Architecture:** Keep config mutation in existing `editForm`; add section/display metadata and derive active section from focused control. Render wide forms with Lip Gloss `JoinHorizontal`, narrow forms with horizontal tabs, and propagate Bubble Tea `WindowSizeMsg` into active form.

**Tech Stack:** Go 1.26.1, Bubble Tea v2, Bubbles textinput v2, Lip Gloss v2, Go `testing`.

## Global Constraints

- Use five process groups exactly: Basic, Environment, Health, Restart, Logging.
- Width `>= 80` uses sidebar; width `< 80` uses horizontal tabs.
- Keep config schema, parsing, validation, controller persistence, critical-edit comparison, process lifecycle, and confirmation flow unchanged.
- Keep explicit environment values masked in every rendered form state.
- Keep `Tab`, `Shift+Tab`, `Up`, `Down`, `Left`, `Right`, `Space`, `Enter`, `Ctrl+X`, `Ctrl+S`, and `Esc` behavior specified in approved design.
- Use installed Bubble Tea, Bubbles, and Lip Gloss dependencies; add no dependency.
- Do not add vertical scrolling in this change.

---

### Task 1: Section-aware form controls

**Files:**
- Modify: `internal/tui/form.go`
- Test: `internal/tui/form_test.go`

**Interfaces:**
- Produces: `formSection`, `formToggle`, metadata-bearing `formField`, `(*editForm).activeSection() formSection`, section-ordered `(*editForm).focusLabels() []string`.
- Preserves: `(*editForm).set`, `value`, `toggle`, `config`, and `configWithoutValidation` signatures.

- [ ] **Step 1: Write failing section-navigation tests**

Add tests to `internal/tui/form_test.go`:

```go
func TestProcessFormGroupsControlsAndCrossesSectionBoundary(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir}}
	form, err := newProcessForm(cfg, 0, -1)
	if err != nil {
		t.Fatal(err)
	}

	labels := form.focusLabels()
	basicEnd := slices.Index(labels, "Autostart")
	environmentStart := slices.Index(labels, "EnvKey")
	if basicEnd < 0 || environmentStart != basicEnd+1 {
		t.Fatalf("focus order = %#v", labels)
	}

	form.focus = basicEnd
	form.moveFocus(1)
	if form.focusLabel() != "EnvKey" || form.activeSection() != environmentSection {
		t.Fatalf("forward focus/section = %q/%v", form.focusLabel(), form.activeSection())
	}
	form.moveFocus(-1)
	if form.focusLabel() != "Autostart" || form.activeSection() != basicSection {
		t.Fatalf("backward focus/section = %q/%v", form.focusLabel(), form.activeSection())
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
```

Add `slices` to test imports.

Update `TestProcessFormMasksEnvironmentValues` to locate form focus through
`slices.Index(form.focusLabels(), "EnvValue")` and
`slices.Index(form.focusLabels(), "EnvKey")`; raw `form.fields` indexes no
longer equal focus indexes once Basic toggles join section order.

- [ ] **Step 2: Run tests and verify red state**

Run: `go test ./internal/tui -run 'TestProcessFormGroupsControlsAndCrossesSectionBoundary|TestShiftTabMovesFormFocusBackward'`

Expected: build failure because `activeSection`, `environmentSection`, and `basicSection` do not exist.

- [ ] **Step 3: Add section and display metadata**

In `internal/tui/form.go`, replace field/control metadata with:

```go
type formSection uint8

const (
	basicSection formSection = iota
	environmentSection
	healthSection
	restartSection
	loggingSection
)

var processSections = []formSection{
	basicSection,
	environmentSection,
	healthSection,
	restartSection,
	loggingSection,
}

type formField struct {
	label   string
	display string
	section formSection
	input   textinput.Model
}

type formToggle struct {
	label   string
	display string
	section formSection
}
```

Add `toggles []formToggle`, `width int`, `height int`, and `creating bool` to `editForm`. Initialize dimensions to `80` and `24` in both constructors. Set `creating` before replacing negative indexes.

Change `addField` to:

```go
func (f *editForm) addField(section formSection, label, display, value string) {
	input := textinput.New()
	input.Prompt = ""
	input.SetWidth(50)
	input.SetValue(value)
	f.fields = append(f.fields, formField{
		label: label, display: display, section: section, input: input,
	})
}
```

Use exact process assignments:

```go
form.addField(basicSection, "Name", "Name", item.Name)
form.addField(basicSection, "Command", "Command", item.Command)
form.addField(basicSection, "Args", "Arguments", string(args))
form.addField(basicSection, "Directory", "Directory", item.Directory)
form.addField(basicSection, "DependsOn", "Depends on", strings.Join(item.DependsOn, ", "))
form.addField(basicSection, "StopTimeout", "Stop timeout", durationString(item.StopTimeout))
form.addField(environmentSection, "EnvKey", "Variable", "")
form.addField(environmentSection, "EnvValue", "Value", "")
form.addField(environmentSection, "EnvFile", "Environment file", item.EnvFile)
form.addField(healthSection, "HealthType", "Health type", item.Health.Type)
form.addField(healthSection, "HealthURL", "URL", item.Health.URL)
form.addField(healthSection, "HealthAddress", "Address", item.Health.Address)
form.addField(healthSection, "HealthTimeout", "Timeout", durationString(item.Health.Timeout))
form.addField(healthSection, "HealthInterval", "Interval", durationString(item.Health.Interval))
form.addField(restartSection, "RestartPolicy", "Policy", item.Restart.Policy)
form.addField(restartSection, "RestartMaxAttempts", "Max attempts", intString(item.Restart.MaxAttempts))
form.addField(restartSection, "RestartWindow", "Window", durationString(item.Restart.Window))
form.addField(restartSection, "InitialBackoff", "Initial backoff", durationString(item.Restart.InitialBackoff))
form.addField(restartSection, "MaxBackoff", "Max backoff", durationString(item.Restart.MaxBackoff))
form.addField(loggingSection, "LogMaxSizeMB", "Max size (MB)", intString(item.Log.MaxSizeMB))
form.addField(loggingSection, "LogMaxFiles", "Max files", intString(item.Log.MaxFiles))
form.addField(loggingSection, "LogBufferLines", "Buffer lines", intString(item.Log.BufferLines))
form.toggles = []formToggle{
	{label: "Shell", display: "Shell", section: basicSection},
	{label: "Autostart", display: "Autostart", section: basicSection},
}
```

Project fields use `basicSection`; project toggles contain only `Autostart`.

- [ ] **Step 4: Make focus order section-aware**

Replace index assumptions with label lookup:

```go
func (f *editForm) sections() []formSection {
	if f.kind == projectForm {
		return []formSection{basicSection}
	}
	return processSections
}

func (f *editForm) focusLabels() []string {
	labels := make([]string, 0, len(f.fields)+len(f.toggles))
	for _, section := range f.sections() {
		for _, field := range f.fields {
			if field.section == section {
				labels = append(labels, field.label)
			}
		}
		for _, toggle := range f.toggles {
			if toggle.section == section {
				labels = append(labels, toggle.label)
			}
		}
	}
	return labels
}

func (f *editForm) fieldIndex(label string) int {
	for index := range f.fields {
		if f.fields[index].label == label {
			return index
		}
	}
	return -1
}

func (f *editForm) activeSection() formSection {
	label := f.focusLabel()
	if index := f.fieldIndex(label); index >= 0 {
		return f.fields[index].section
	}
	for _, toggle := range f.toggles {
		if toggle.label == label {
			return toggle.section
		}
	}
	return basicSection
}
```

Update `moveFocus` to blur/focus via `fieldIndex(f.focusLabel())`. Update text input dispatch and `cycleEnum` the same way. Handle `Shift+Tab` before plain `Tab`:

```go
if key.Code == tea.KeyTab && key.Mod == tea.ModShift {
	f.moveFocus(-1)
	return nil
}
switch key.Code {
case tea.KeyTab, tea.KeyDown:
	f.moveFocus(1)
	return nil
case tea.KeyUp:
	f.moveFocus(-1)
	return nil
}
```

Prevent arbitrary text updates while `HealthType` or `RestartPolicy` is focused; these controls only change through `Left` and `Right`.

- [ ] **Step 5: Run focused and existing form tests**

Run: `go test ./internal/tui -run 'TestProcessForm|TestProjectForm|TestFormKeyboard|TestShiftTab'`

Expected: PASS.

- [ ] **Step 6: Commit section-aware controls**

Run:

```bash
git add internal/tui/form.go internal/tui/form_test.go
git commit -m "refactor: group process form controls"
```

---

### Task 2: Responsive modern renderer

**Files:**
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/form.go`
- Test: `internal/tui/form_test.go`

**Interfaces:**
- Consumes: `(*editForm).activeSection()`, field display and section metadata from Task 1.
- Produces: `(*editForm).resize(width, height int)`, responsive `(*editForm).view() string`.

- [ ] **Step 1: Write failing renderer tests**

Add:

```go
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
	for _, want := range []string{"New process", "▸ Basic", "Command", "Arguments", "✓", "Autostart"} {
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
```

Update `TestFormKeyboardTogglesFocusableBooleans` to assert `✓ Autostart` or `✓ Shell` rather than old `› Shell: true` text.

- [ ] **Step 2: Run renderer tests and verify red state**

Run: `go test ./internal/tui -run 'TestProcessFormRenders|TestFormKeyboardToggles'`

Expected: build failure because `resize` does not exist, then assertion failures until modern renderer exists.

- [ ] **Step 3: Add form styles**

Append these styles to `internal/tui/styles.go`:

```go
formFrameStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("8")).
	Padding(0, 1)
formHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
formMutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
formSectionStyle = lipgloss.NewStyle().Padding(0, 1)
formActiveSectionStyle = formSectionStyle.Bold(true).
	Foreground(lipgloss.Color("12")).
	Background(lipgloss.Color("236"))
formInputStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("8")).
	Padding(0, 1)
formFocusedInputStyle = formInputStyle.BorderForeground(lipgloss.Color("12"))
formToggleStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("8")).
	Padding(0, 1)
formEnabledToggleStyle = formToggleStyle.Foreground(lipgloss.Color("10")).Bold(true)
formErrorStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("9")).
	Foreground(lipgloss.Color("9")).
	Padding(0, 1)
```

- [ ] **Step 4: Implement responsive renderer**

Add section names and safe dimensions:

```go
func (s formSection) String() string {
	switch s {
	case environmentSection:
		return "Environment"
	case healthSection:
		return "Health"
	case restartSection:
		return "Restart"
	case loggingSection:
		return "Logging"
	default:
		return "Basic"
	}
}

func (f *editForm) resize(width, height int) {
	f.width = max(width, 1)
	f.height = max(height, 1)
}

func (f *editForm) header() string {
	noun := "project"
	if f.kind == processForm {
		noun = "process"
	}
	if f.creating {
		return "New " + noun
	}
	name := strings.TrimSpace(f.value("Name"))
	if name == "" {
		return "Edit " + noun
	}
	return "Edit " + noun + " · " + name
}
```

Implement `view` with these exact layout rules:

```go
func (f *editForm) view() string {
	width := max(f.width-4, 1)
	bodyWidth := max(width-4, 1)
	var body string
	if f.kind == processForm && f.width >= 80 {
		sidebar := f.renderSidebar(16)
		panelWidth := max(bodyWidth-lipgloss.Width(sidebar)-2, 24)
		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, "  ", f.renderPanel(panelWidth))
	} else {
		if f.kind == processForm {
			body = f.renderTabs(bodyWidth) + "\n\n"
		}
		body += f.renderPanel(bodyWidth)
	}

	content := formHeaderStyle.Render(f.header()) + "\n" + formMutedStyle.Render(f.activeSection().String()) + "\n\n" + body
	if f.err != nil {
		content += "\n" + formErrorStyle.Width(max(bodyWidth-2, 1)).Render(f.err.Error())
	}
	content += "\n\n" + formMutedStyle.Render(f.footer())
	return formFrameStyle.Width(width).Render(content)
}
```

`renderSidebar` renders all five section names, active row as `▸ ` plus `formActiveSectionStyle`, and inactive rows as two spaces plus `formSectionStyle`. `renderTabs` renders active section as `[ Name ]`, inactive sections as `  Name  `, joining with one space. Project form omits both.

`renderPanel` iterates only fields and toggles whose section equals `activeSection()`. Each field renders friendly label on one line and value/control on next. Use `formFocusedInputStyle` only for focused label. Copy each textinput before rendering and set copied input width to `max(panelWidth-6, 1)` so rendering does not mutate form state. Outer frame width remains bounded by terminal width; horizontal tabs may wrap naturally at very small widths.

Render enums without editable text-input chrome:

```go
func (f *editForm) renderField(field formField, width int) string {
	label := formMutedStyle.Render(strings.ToUpper(field.display))
	focused := f.focusLabel() == field.label
	value := field.input.View()
	if field.label == "HealthType" || field.label == "RestartPolicy" {
		value = "‹ " + field.input.Value() + " ›"
	}
	style := formInputStyle
	if focused {
		style = formFocusedInputStyle
	}
	return label + "\n" + style.Width(max(width-4, 1)).Render(value)
}
```

For `EnvKey`, append sorted masked keys below input as `KEY=••••`; never render map values. For toggles, render `✓ Label` when true and `○ Label` when false. Apply focused accent styling independently of enabled state.

Footer text stays `Ctrl+S save  Esc cancel  Tab next`; process forms append `  Enter set env  Ctrl+X delete env` only in Environment section.

- [ ] **Step 5: Run renderer and security tests**

Run: `go test ./internal/tui -run 'TestProcessFormRenders|TestProcessFormMasksEnvironmentValues|TestFormKeyboardToggles'`

Expected: PASS, including absence of `secret-value` and `replacement-secret` in output.

- [ ] **Step 6: Run all TUI tests**

Run: `go test ./internal/tui`

Expected: PASS.

- [ ] **Step 7: Commit renderer**

Run:

```bash
git add internal/tui/form.go internal/tui/styles.go internal/tui/form_test.go
git commit -m "feat: modernize process form layout"
```

---

### Task 3: Resize wiring and full verification

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/form_test.go`

**Interfaces:**
- Consumes: `(*editForm).resize(width, height int)` from Task 2.
- Preserves: `Model.Update(msg tea.Msg) (tea.Model, tea.Cmd)` and `Model.View() tea.View`.

- [ ] **Step 1: Write failing resize propagation test**

Add:

```go
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
```

If `tea.View.Content` is not directly accessible in pinned Bubble Tea v2, assert `got.form.view()` instead; keep size assertion unchanged.

- [ ] **Step 2: Run test and verify red state**

Run: `go test ./internal/tui -run TestModelPropagatesWindowSizeToOpenForm`

Expected: FAIL because form remains at constructor size `80x24`.

- [ ] **Step 3: Propagate dimensions**

In `tea.WindowSizeMsg` handling, add:

```go
if m.form != nil {
	m.form.resize(m.width, m.height)
}
```

After each successful form constructor in `openProjectForm` and `openProcessForm`, add:

```go
form.resize(m.width, m.height)
```

This ensures both already-open forms and forms opened after a resize use current dimensions.

- [ ] **Step 4: Run focused test**

Run: `go test ./internal/tui -run TestModelPropagatesWindowSizeToOpenForm`

Expected: PASS.

- [ ] **Step 5: Run repository verification**

Run: `gofmt -w internal/tui/form.go internal/tui/styles.go internal/tui/model.go internal/tui/form_test.go`

Run: `go test ./...`

Run: `go vet ./...`

Expected: all commands exit `0`.

- [ ] **Step 6: Check deliberate security and scope constraints**

Run: `git diff --check`

Run: `git diff -- go.mod go.sum`

Expected: no whitespace errors; no dependency changes.

Run: `go test ./internal/tui -run TestProcessFormMasksEnvironmentValues -count=1`

Expected: PASS.

- [ ] **Step 7: Commit resize wiring and design docs**

Run:

```bash
git add internal/tui/model.go internal/tui/form_test.go docs/superpowers/specs/2026-07-28-modern-process-form-design.md docs/superpowers/plans/2026-07-28-modern-process-form.md
git commit -m "test: verify responsive process form"
```
