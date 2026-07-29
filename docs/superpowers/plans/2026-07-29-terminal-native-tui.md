# Terminal-Native TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace heavy raised-surface TUI styling with compact terminal-native presentation across dashboard, log viewer, forms, and overlays without changing behavior.

**Architecture:** Keep existing Bubble Tea state, responsive geometry, event routing, and Lip Gloss layer composition. Centralize visual change in existing semantic styles; bottom-only control borders create restrained value lines while focused controls gain stronger non-color-only boundaries. Preserve exact screen sizing through existing render and `fitScreen` paths.

**Tech Stack:** Go 1.26.1, Bubble Tea v2.0.8, Bubbles v2.1.1, Lip Gloss v2.0.5, Go `testing`.

## Global Constraints

- Add no dependency; `go.mod` and `go.sum` must not change.
- Use one dark terminal background across dashboard, log viewer, forms, and overlays; no second full-area raised surface.
- Cyan marks selection, focus, and active navigation only.
- Green marks running, healthy, or enabled state; yellow marks transitional state and destructive confirmation titles; red marks failed state and validation or operation errors; muted gray marks labels, inactive navigation, stopped state, separators, and borders.
- Selection keeps `›`; toggles keep `[ ON  ]` and `[ OFF ]`; focus styling must not rely only on hue.
- Preserve services, controllers, config, process lifecycle, log data flow, keyboard controls, validation, field errors, summary errors, and environment secret masking.
- Preserve dashboard breakpoints exactly: wide at `>= 100`, medium at `70–99`, narrow below `70` columns.
- Preserve form sizing exactly: `min(90, terminalWidth-4)` by `terminalHeight-4` at `60x16` or larger; smaller terminals use full screen.
- Preserve wide form sidebar at modal width `>= 80`, narrow tabs below `80`, viewport scrolling, exact terminal dimensions, and overlay priority.
- Avoid rounded web cards, wide colored slabs, gradients, shadows, and decorative chrome.
- Tests use existing Go `testing` and ANSI assertions; add no snapshot package.
- `ponytail:` presentation-only redesign. Add user-selectable themes only when users need multiple terminal palettes.

## File Structure

- Modify `internal/tui/styles.go`: define shared terminal-native semantic styles; remove raised backgrounds from chrome, selection, overlays, active form navigation, and modal whitespace.
- Modify `internal/tui/model_test.go`: cover shared dashboard and overlay palette while retaining exact-size and layering assertions.
- Modify `internal/tui/logview_test.go`: cover shared log-viewer palette while retaining log behavior and exact-size assertions.
- Modify `internal/tui/form_test.go`: cover modal background, focus hierarchy, labels, responsive size, and existing secret/error behavior.
- Leave `internal/tui/dashboard.go`, `internal/tui/logview.go`, `internal/tui/overlay.go`, and `internal/tui/model.go` unchanged: their existing geometry, composition, and semantic-style use already provide required propagation.

---

### Task 1: Shared Dashboard, Log, and Overlay Chrome

**Files:**
- Modify: `internal/tui/styles.go:5-76`
- Test: `internal/tui/model_test.go:21-129,237-256`
- Test: `internal/tui/logview_test.go:79-95`

**Interfaces:**
- Consumes: existing `renderDashboard`, `logView.render`, `renderProjectMenu`, `composeOverlay`, and `fitScreen` output.
- Produces: background-free `appHeaderStyle`, `appFooterStyle`, `selectionStyle`, and `overlayStyle`; muted pane and overlay boundaries; unchanged color constant names used throughout package.
- Preserves: all render function signatures, dashboard geometry, exact-size behavior, status styles, overlay centering, and input routing.

- [x] **Step 1: Add ANSI helpers and failing shared-palette tests**

In `internal/tui/model_test.go`, extend helper block after `stripANSI`:

```go
const (
	ansiSurface = "236"
	ansiRaised  = "238"
	ansiAccent  = "96"
	ansiMuted   = "90"
	ansiSuccess = "92"
)

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
```

Add dashboard test after `TestDashboardUsesWholeTerminalAndShowsPID`:

```go
func TestDashboardUsesTerminalNativePalette(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	view := model.View().Content
	assertTerminalNativePalette(t, view)
	if !hasANSICode(view, ansiSuccess) || !strings.Contains(stripANSI(view), "› api") {
		t.Fatalf("semantic state or selection marker missing: %q", view)
	}
}
```

Add overlay test after `TestProjectMenuRendersCenteredWithoutGrowingScreen`:

```go
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
```

In `internal/tui/logview_test.go`, replace `TestLogViewerUsesOperationsChromeAndWholeTerminal` name with `TestLogViewerUsesTerminalNativeChromeAndWholeTerminal`, then add one assertion after exact-size check:

```go
	assertTerminalNativePalette(t, view)
```

- [x] **Step 2: Run focused tests and verify red state**

Run:

```bash
go test ./internal/tui -run 'TestDashboardUsesTerminalNativePalette|TestProjectMenuUsesCompactTerminalNativePalette|TestLogViewerUsesTerminalNativeChromeAndWholeTerminal' -count=1
```

Expected: FAIL. ANSI parser finds code `238` from current header, footer, selection, and overlay backgrounds.

- [x] **Step 3: Remove raised backgrounds from shared semantic styles**

In `internal/tui/styles.go`, keep exact color constants:

```go
const (
	colorAccent      = "14"
	colorMuted       = "8"
	colorSuccess     = "10"
	colorError       = "9"
	colorWarning     = "11"
	colorSurface     = "236"
	colorSurfaceHigh = "238"
```

Replace shared chrome, selection, and overlay style declarations with:

```go
	appHeaderStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(colorAccent))
	appFooterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorMuted))
	paneStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorMuted)).Padding(0, paneHorizontalPadding)
	paneTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorMuted))
	selectionStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(colorAccent))
```

```go
	overlayStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorMuted)).
			Padding(1, 2)
```

Do not alter `runningStyle`, `transitionalStyle`, `stoppedStyle`, `failedStyle`, `errorStyle`, `overlayTitleStyle`, `confirmTitleStyle`, form styles, or `fitScreen`. Root `colorSurface` remains shared terminal background; `colorSurfaceHigh` remains declared because Task 2 still needs it until form styles change.

- [x] **Step 4: Run shared TUI regression tests**

Run:

```bash
go test ./internal/tui -run 'TestDashboard|TestLog|TestProjectMenu|TestOverlay|TestConfirmation' -count=1
```

Expected: PASS. Existing exact terminal size, breakpoints, content, overlay centering, and keyboard behavior remain unchanged.

- [x] **Step 5: Commit shared terminal-native chrome**

```bash
git add internal/tui/styles.go internal/tui/model_test.go internal/tui/logview_test.go
git commit -m "style(tui): simplify terminal chrome" -m "Remove raised fills from shared dashboard, log, and overlay styles while preserving semantic status colors and exact layouts."
```

---

### Task 2: Terminal-Native Form Hierarchy

**Files:**
- Modify: `internal/tui/styles.go:5-76`
- Test: `internal/tui/form_test.go:218-357,439-569`

**Interfaces:**
- Consumes: existing `editForm.view() string`, `(*editForm).renderField(field formField, width int) string`, `(*editForm).renderToggle(toggle formToggle) string`, and semantic form styles from Task 1.
- Produces: `formInputStyle` using muted normal bottom border, `formFocusedInputStyle` using cyan thick bottom border, background-free `formActiveSectionStyle` and `formModalStyle`, and compact unfilled `formErrorStyle`.
- Preserves: all form method signatures, modal dimensions, sidebar/tab breakpoints, field order, viewport range calculations, enum controls, toggle markers, validation placement, summary placement, cursor editing, and secret masking.

- [x] **Step 1: Add failing form palette and hierarchy tests**

Tests stay in package `tui`, separate from package `tui_test`; add local ANSI parsing helpers after imports:

```go
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
```

Add `regexp` to existing imports. Add tests after `TestFormModalUsesCalculatedSize`:

```go
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
```

The existing `TestProcessFormNarrowLongValueUsesNarrowInputViewport` expects five lines from a fully boxed control. Update expected compact value-line height:

```go
	if got := strings.Count(rendered, "\n") + 1; got != 3 {
		t.Fatalf("rendered field height = %d, want 3: %q", got, rendered)
	}
```

- [x] **Step 2: Run focused tests and verify red state**

Run:

```bash
go test ./internal/tui -run 'TestFormModalUsesRootBackgroundWithoutRaisedFill|TestFormFocusedFieldHasStrongerBoundaryThanUnfocusedField|TestProcessFormNarrowLongValueUsesNarrowInputViewport' -count=1
```

Expected: FAIL. ANSI parser finds code `238` from current modal background; focused and unfocused fields both use normal full boxes; narrow field remains five lines.

- [x] **Step 3: Change form controls to restrained value lines**

In `internal/tui/styles.go`, remove filled active navigation and modal styles:

```go
	formActiveSectionStyle = formSectionStyle.Bold(true).
			Foreground(lipgloss.Color(colorAccent))
```

```go
	formModalStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(colorMuted))
```

Replace input and error styles with:

```go
	formInputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color(colorMuted)).
			Padding(0, 1)
	formFocusedInputStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color(colorAccent)).
			Padding(0, 1)
	formInlineErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	formSwitchStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	formEnabledSwitchStyle = formSwitchStyle.Foreground(lipgloss.Color(colorSuccess)).Bold(true)
	formErrorStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color(colorError)).
			Foreground(lipgloss.Color(colorError)).
			Padding(0, cardHorizontalPadding)
```

Keep `renderField` width calculations and `renderToggle` text unchanged. Bottom border changes each text/select control from a four-sided box to label, value line, and one boundary line; thick focused border makes focus visible by shape as well as cyan. Existing `panelContent` blank lines remain one-row group spacing.

Delete unused `colorSurfaceHigh` from constants after confirming no production references remain:

```go
	colorSurface = "236"
```

Run reference check:

```bash
rg 'colorSurfaceHigh|48;5;238' internal/tui --glob '*.go'
```

Expected: only test literals `ansiRaised = "238"` and `formHasANSICode(view, "238")`; no production reference.

- [x] **Step 4: Run form tests and preserve behavior**

Run:

```bash
go test ./internal/tui -run 'TestForm|TestProcessForm|TestProjectForm|TestText|TestUpDown|TestShiftTab|TestActiveProcess' -count=1
```

Expected: PASS, including modal dimensions, tiny-terminal fallback, sidebar/tabs, viewport focus visibility, enum cycling, inline and summary errors, cursor editing, toggle semantics, critical-save lifecycle, and environment secret masking.

- [x] **Step 5: Commit compact form hierarchy**

```bash
git add internal/tui/styles.go internal/tui/form_test.go
git commit -m "style(tui): lighten form hierarchy" -m "Use root modal background and compact value-line controls with stronger shape-based focus boundaries."
```

---

### Task 3: Full Regression Verification

**Files:**
- Verify: `internal/tui/styles.go`
- Verify unchanged: `internal/tui/form.go`
- Verify: `internal/tui/model_test.go`
- Verify: `internal/tui/logview_test.go`
- Verify: `internal/tui/form_test.go`
- Verify unchanged: `go.mod`, `go.sum`, `internal/tui/dashboard.go`, `internal/tui/logview.go`, `internal/tui/overlay.go`, `internal/tui/model.go`

**Interfaces:**
- Consumes: completed shared style and form hierarchy changes from Tasks 1–2.
- Produces: formatted, repository-wide verified presentation-only redesign.
- Preserves: all public and package-private production signatures.

- [x] **Step 1: Format changed Go files**

Run:

```bash
gofmt -w internal/tui/styles.go internal/tui/model_test.go internal/tui/logview_test.go internal/tui/form_test.go
```

Expected: command exits `0`.

- [x] **Step 2: Verify focused visual regression tests**

Run:

```bash
go test ./internal/tui -run 'TestDashboardUsesTerminalNativePalette|TestProjectMenuUsesCompactTerminalNativePalette|TestLogViewerUsesTerminalNativeChromeAndWholeTerminal|TestFormModalUsesRootBackgroundWithoutRaisedFill|TestFormFocusedFieldHasStrongerBoundaryThanUnfocusedField' -count=1
```

Expected: PASS.

- [x] **Step 3: Verify full repository**

Run:

```bash
go test ./... -count=1
```

Expected: all packages PASS.

- [x] **Step 4: Verify scope, formatting, and dependency constraints**

Run:

```bash
git diff --check
git --no-pager diff --stat b2456c2..HEAD
git --no-pager diff --name-only b2456c2..HEAD
git --no-pager diff b2456c2..HEAD -- go.mod go.sum internal/tui/dashboard.go internal/tui/logview.go internal/tui/overlay.go internal/tui/model.go
rg 'colorSurfaceHigh|48;5;238' internal/tui --glob '*.go'
```

Expected:

- `git diff --check` prints nothing.
- Changed implementation scope is `internal/tui/styles.go`; changed tests are `internal/tui/model_test.go`, `internal/tui/logview_test.go`, and `internal/tui/form_test.go`.
- Dependency and unchanged-renderer diff prints nothing.
- Raised-surface search finds only test regression constants, never production code.
- Pre-existing untracked `.vscode/` and `docs/superpowers/specs/2026-07-28-semantic-constants-refactor-design.md` remain untouched.

- [x] **Step 5: Commit formatting only if Step 1 changed tracked files after Task 2 commit**

Check:

```bash
git --no-pager status --short
```

If tracked Go files remain modified, commit only those tracked files:

```bash
git add internal/tui/styles.go internal/tui/model_test.go internal/tui/logview_test.go internal/tui/form_test.go
git commit -m "test(tui): verify terminal-native rendering"
```

If no tracked files remain modified, do not create an empty commit.
