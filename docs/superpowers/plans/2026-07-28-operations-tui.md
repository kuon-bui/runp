# Operations TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace card dashboard and full-screen forms with responsive operations console showing PID and selected-process logs, plus web-like modal forms with cursor-safe editing.

**Architecture:** Keep Bubble Tea `Model` as sole runtime state owner. Add reusable log-preview state beside existing full log viewer, render dashboard from pure snapshot/preview inputs, and compose centered modal layers with Lip Gloss canvas. Keep config and process services unchanged; PID already arrives through `ProcessSnapshot.Runtime.PID`.

**Tech Stack:** Go 1.26.1, Bubble Tea v2.0.8, Bubbles v2.1.1, Lip Gloss v2.0.5, Go `testing`.

## Global Constraints

- Add no dependency; `go.mod` and `go.sum` must not change.
- Preserve config schema, validation, lifecycle, logging, critical-edit confirmation, and secret masking.
- Exclude CPU/memory collection, mouse interaction, and controller/process changes.
- Use cyan active state, green running, yellow transitional, red failed/blocked, gray stopped.
- Root output must fit latest terminal width and height; clamp each dimension to at least one cell.
- Wide dashboard starts at 100 columns, medium at 70 columns, narrow below 70 columns.
- Wide project pane uses 20% clamped to 16–24 columns; process pane uses 38% clamped to 30–46 columns.
- Modal uses `min(90, terminalWidth-4)` by `terminalHeight-4` at 60×16 or larger; smaller terminals use full screen.
- Text fields reserve `Left`, `Right`, `Home`, `End`, `Backspace`, and `Delete` for cursor editing.
- Tests use Go `testing` and content assertions after ANSI stripping; add no snapshot package.

## File Structure

- Modify `internal/tui/dashboard.go`: responsive geometry, full-terminal panes, state/PID rows, app header/footer.
- Modify `internal/tui/logview.go`: shared record formatting, dashboard preview, operations-styled full viewer.
- Modify `internal/tui/model.go`: preview lifecycle, event refresh, key-layer routing, modal composition.
- Modify `internal/tui/form.go`: cursor-safe key routing, web-like controls, field errors, form viewport.
- Modify `internal/tui/styles.go`: operations palette and semantic pane, overlay, modal, and control styles.
- Create `internal/tui/overlay.go`: exact-size centered layer composition.
- Create `internal/tui/logview_internal_test.go`: package-internal formatter and preview tests.
- Modify `internal/tui/model_test.go`, `internal/tui/form_test.go`, and `internal/tui/logview_test.go`.
- Modify `README.md`: describe dashboard and modal form presentation; existing key lists remain.

---

### Task 1: Shared Log Formatting and Preview

**Files:**
- Modify: `internal/tui/logview.go`
- Create: `internal/tui/logview_internal_test.go`

**Interfaces:**
- Consumes: `Services.LogSnapshot`, `logstore.Record`, `viewport.Model`.
- Produces: `formatLogRecords(records []logstore.Record) []string`, `logPreview`, `newLogPreview(width, height int) logPreview`, and preview methods used by Task 2.

- [ ] **Step 1: Write failing formatter and preview tests**

Create `internal/tui/logview_internal_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify red state**

Run:

```bash
go test ./internal/tui -run 'TestFormatLogRecords|TestLogPreview' -count=1
```

Expected: build failure because `formatLogRecords` and `newLogPreview` do not exist.

- [ ] **Step 3: Add shared formatter and preview state**

In `internal/tui/logview.go`, add:

```go
func formatLogRecords(records []logstore.Record) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		stream := "OUT"
		if record.Stream == logstore.Stderr {
			stream = "ERR"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s", record.At.Local().Format(logTimeFormat), stream, record.Text))
	}
	return lines
}

type logPreview struct {
	project  string
	process  string
	viewport viewport.Model
}

func newLogPreview(width, height int) logPreview {
	view := viewport.New(
		viewport.WithWidth(max(width, 1)),
		viewport.WithHeight(max(height, 1)),
	)
	view.FillHeight = true
	return logPreview{viewport: view}
}

func (l *logPreview) show(project, process string, services Services) {
	l.project, l.process = project, process
	l.refresh(services)
}

func (l *logPreview) refresh(services Services) {
	var records []logstore.Record
	if l.project != "" && l.process != "" && services.LogSnapshot != nil {
		records = services.LogSnapshot(l.project, l.process)
	}
	lines := formatLogRecords(records)
	if len(lines) == 0 {
		lines = []string{"Waiting for output…"}
	}
	l.viewport.SetContentLines(lines)
	l.viewport.GotoBottom()
}

func (l *logPreview) resize(width, height int) {
	l.viewport.SetWidth(max(width, 1))
	l.viewport.SetHeight(max(height, 1))
	l.viewport.GotoBottom()
}

func (l logPreview) matches(event logstore.Event) bool {
	return event.Project == l.project && event.Process == l.process
}

func (l logPreview) render() string { return l.viewport.View() }
```

Replace `logView.refresh` inline record loop with:

```go
l.viewport.SetContentLines(formatLogRecords(records))
```

- [ ] **Step 4: Run focused and existing log tests**

Run:

```bash
go test ./internal/tui -run 'TestFormatLogRecords|TestLogPreview|TestLog' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit shared log state**

```bash
git add internal/tui/logview.go internal/tui/logview_internal_test.go
git commit -m "refactor(tui): share log rendering"
```

---

### Task 2: Responsive Operations Dashboard

**Files:**
- Modify: `internal/tui/dashboard.go`
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `logPreview` from Task 1 and `ProcessSnapshot.Runtime.PID`.
- Produces: `dashboardGeometry`, `dashboardLayout(width, height int) dashboardGeometry`, `fitScreen(content string, width, height int) string`, and `renderDashboard(snapshot controller.Snapshot, projectIndex, processIndex int, preview string, width, height int) string`.

- [ ] **Step 1: Expand fixture and write failing dashboard tests**

Update `dashboardFixture` runtime values:

```go
{Name: "api", Runtime: process.Snapshot{State: process.Running, PID: 1832}},
{Name: "web", Runtime: process.Snapshot{State: process.Stopped}},
```

Add imports for `fmt`, `time`, Lip Gloss, and `runp/internal/logstore` to `internal/tui/model_test.go`. Replace local ANSI removal with:

```go
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func stripANSI(value string) string { return ansiPattern.ReplaceAllString(value, "") }
```

Add tests:

```go
func TestDashboardUsesWholeTerminalAndShowsPID(t *testing.T) {
	model := tui.New(tui.Services{Snapshots: dashboardFixture})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 120, Height: 30})
	view := model.View().Content
	if width, height := lipgloss.Size(view); width != 120 || height != 30 {
		t.Fatalf("screen = %dx%d", width, height)
	}
	plain := stripANSI(view)
	for _, want := range []string{"PROJECTS", "PROCESSES", "LIVE LOG", "1832", "—"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("view missing %q: %q", want, plain)
		}
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
```

- [ ] **Step 2: Run dashboard tests to verify red state**

```bash
go test ./internal/tui -run 'TestDashboard' -count=1
```

Expected: failures for card layout, missing PID/log pane, wrong dimensions, and stale preview.

- [ ] **Step 3: Replace card constants and styles**

In `internal/tui/styles.go`, replace card-layout values with:

```go
const (
	colorAccent      = "14"
	colorMuted       = "8"
	colorSuccess     = "10"
	colorError       = "9"
	colorWarning     = "11"
	colorSurface     = "236"
	colorSurfaceHigh = "238"

	defaultTerminalWidth  = 80
	defaultTerminalHeight = 24
	wideDashboardWidth    = 100
	mediumDashboardWidth  = 70
	projectPaneMinWidth   = 16
	projectPaneMaxWidth   = 24
	processPaneMinWidth   = 30
	processPaneMaxWidth   = 46
	rootChromeHeight      = 2
	paneFrameWidth        = 2
	paneHorizontalPadding = 1
)

var (
	appHeaderStyle = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorSurfaceHigh))
	appFooterStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorMuted)).Background(lipgloss.Color(colorSurfaceHigh))
	paneStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colorMuted)).Padding(0, paneHorizontalPadding)
	paneTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorMuted))
	selectionStyle = lipgloss.NewStyle().Bold(true).
		Foreground(lipgloss.Color(colorAccent)).Background(lipgloss.Color(colorSurfaceHigh))
	runningStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorSuccess))
	transitionalStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorWarning))
	stoppedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
	failedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorError))
)
```

Keep form style names compiling; Task 4 changes their visual treatment.

- [ ] **Step 4: Implement exact geometry and rendering helpers**

In `internal/tui/dashboard.go`, replace card rendering with:

```go
type dashboardMode uint8

const (
	dashboardNarrow dashboardMode = iota
	dashboardMedium
	dashboardWide
)

type dashboardGeometry struct {
	mode                                dashboardMode
	bodyHeight, processHeight           int
	projectWidth, processWidth          int
	logWidth, logHeight, logBodyHeight  int
}

func dashboardLayout(width, height int) dashboardGeometry {
	width, height = max(width, 1), max(height, 1)
	bodyHeight := max(height-rootChromeHeight, 1)
	if width >= wideDashboardWidth {
		projects := min(max(width*20/100, projectPaneMinWidth), projectPaneMaxWidth)
		processes := min(max(width*38/100, processPaneMinWidth), processPaneMaxWidth)
		return dashboardGeometry{
			mode: dashboardWide, bodyHeight: bodyHeight, processHeight: bodyHeight,
			projectWidth: projects, processWidth: processes,
			logWidth: max(width-projects-processes, 1), logHeight: bodyHeight,
			logBodyHeight: max(bodyHeight-paneFrameWidth-1, 1),
		}
	}
	if width >= mediumDashboardWidth {
		left := min(max(width*42/100, processPaneMinWidth), processPaneMaxWidth)
		return dashboardGeometry{
			mode: dashboardMedium, bodyHeight: bodyHeight, processHeight: bodyHeight,
			processWidth: left, logWidth: max(width-left, 1), logHeight: bodyHeight,
			logBodyHeight: max(bodyHeight-paneFrameWidth-1, 1),
		}
	}
	processHeight := max(bodyHeight/3, 3)
	logHeight := max(bodyHeight-processHeight, 1)
	return dashboardGeometry{
		mode: dashboardNarrow, bodyHeight: bodyHeight, processHeight: processHeight,
		processWidth: width, logWidth: width, logHeight: logHeight,
		logBodyHeight: max(logHeight-paneFrameWidth-1, 1),
	}
}

func fitScreen(content string, width, height int) string {
	width, height = max(width, 1), max(height, 1)
	return lipgloss.NewStyle().Width(width).Height(height).
		MaxWidth(width).MaxHeight(height).
		Background(lipgloss.Color(colorSurface)).Render(content)
}

func renderVisibleLines(lines []string, selected, width, height int) string {
	view := viewport.New(
		viewport.WithWidth(max(width, 1)),
		viewport.WithHeight(max(height, 1)),
	)
	view.SetContentLines(lines)
	if selected >= 0 {
		view.SetYOffset(min(max(selected-height+1, 0), max(len(lines)-height, 0)))
	}
	return view.View()
}

func renderPane(title, body string, width, height int) string {
	innerWidth := max(width-paneFrameWidth-2*paneHorizontalPadding, 1)
	innerHeight := max(height-paneFrameWidth, 1)
	content := paneTitleStyle.MaxWidth(innerWidth).Render(title)
	if innerHeight > 1 {
		content += "\n" + lipgloss.NewStyle().MaxWidth(innerWidth).MaxHeight(innerHeight-1).Render(body)
	}
	return paneStyle.Width(innerWidth).Height(innerHeight).
		MaxWidth(width).MaxHeight(height).Render(content)
}

func renderPID(pid int) string {
	if pid <= 0 {
		return "—"
	}
	return strconv.Itoa(pid)
}
```

Update `renderState`:

```go
switch state {
case process.Running:
	return runningStyle.Render(label)
case process.Starting, process.Stopping, process.Restarting:
	return transitionalStyle.Render(label)
case process.Failed, process.Blocked:
	return failedStyle.Render(label)
default:
	return stoppedStyle.Render(label)
}
```

- [ ] **Step 5: Add row, header, and responsive assembly functions**

Add exact row helpers:

```go
func projectRows(snapshot controller.Snapshot, selected int) []string {
	rows := make([]string, 0, len(snapshot.Projects))
	for index, project := range snapshot.Projects {
		marker := "  "
		if index == selected {
			marker = "› "
		}
		row := marker + project.Name
		if index == selected {
			row = selectionStyle.Render(row)
		}
		rows = append(rows, row)
	}
	return rows
}

func processRow(item controller.ProcessSnapshot, selected bool, width int) string {
	nameWidth := max(width-20, 1)
	marker := "  "
	if selected {
		marker = "› "
	}
	name := lipgloss.NewStyle().Inline(true).MaxWidth(nameWidth).Render(item.Name)
	state := lipgloss.NewStyle().Width(10).MaxWidth(10).Render(renderState(item.Runtime.State))
	row := fmt.Sprintf("%s%-*s %s %5s", marker, nameWidth, name, state, renderPID(item.Runtime.PID))
	if selected {
		row = selectionStyle.MaxWidth(width).Render(row)
	}
	return row
}

func currentProcessRows(snapshot controller.Snapshot, projectIndex, processIndex, width int) []string {
	if projectIndex < 0 || projectIndex >= len(snapshot.Projects) {
		return nil
	}
	items := snapshot.Projects[projectIndex].Processes
	rows := make([]string, 0, len(items))
	for index, item := range items {
		rows = append(rows, processRow(item, index == processIndex, width))
	}
	return rows
}

func groupedProcessRows(snapshot controller.Snapshot, projectIndex, processIndex, width int) ([]string, int) {
	rows := make([]string, 0)
	selectedLine := -1
	for currentProject, project := range snapshot.Projects {
		rows = append(rows, paneTitleStyle.Render(strings.ToUpper(project.Name)))
		for currentProcess, item := range project.Processes {
			selected := currentProject == projectIndex && currentProcess == processIndex
			if selected {
				selectedLine = len(rows)
			}
			rows = append(rows, processRow(item, selected, width))
		}
	}
	return rows, selectedLine
}

func processCounts(snapshot controller.Snapshot) (running, total int) {
	for _, project := range snapshot.Projects {
		for _, item := range project.Processes {
			total++
			if item.Runtime.State == process.Running {
				running++
			}
		}
	}
	return running, total
}
```

Add `renderAppHeader`; include selected context unless it makes line too wide, then drop context before counts. Assemble wide panes horizontally, medium grouped process/log panes horizontally, narrow process/log panes vertically. Final function signature and return:

```go
func renderDashboard(snapshot controller.Snapshot, projectIndex, processIndex int, preview string, width, height int) string {
	geometry := dashboardLayout(width, height)
	header := renderAppHeader(snapshot, projectIndex, processIndex, width)
	footerLine := appFooterStyle.Width(width).MaxHeight(1).Render(footer(width))
	var body string
	switch geometry.mode {
	case dashboardWide:
		projectWidth := max(geometry.projectWidth-paneFrameWidth-2*paneHorizontalPadding, 1)
		processWidth := max(geometry.processWidth-paneFrameWidth-2*paneHorizontalPadding, 1)
		projects := renderVisibleLines(projectRows(snapshot, projectIndex), projectIndex, projectWidth, max(geometry.bodyHeight-paneFrameWidth-1, 1))
		processes := renderVisibleLines(currentProcessRows(snapshot, projectIndex, processIndex, processWidth), processIndex, processWidth, max(geometry.processHeight-paneFrameWidth-1, 1))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			renderPane("PROJECTS", projects, geometry.projectWidth, geometry.bodyHeight),
			renderPane("PROCESSES", processes, geometry.processWidth, geometry.bodyHeight),
			renderPane("LIVE LOG", preview, geometry.logWidth, geometry.logHeight),
		)
	case dashboardMedium:
		processWidth := max(geometry.processWidth-paneFrameWidth-2*paneHorizontalPadding, 1)
		rows, selectedLine := groupedProcessRows(snapshot, projectIndex, processIndex, processWidth)
		processes := renderVisibleLines(rows, selectedLine, processWidth, max(geometry.processHeight-paneFrameWidth-1, 1))
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			renderPane("PROCESSES", processes, geometry.processWidth, geometry.bodyHeight),
			renderPane("LIVE LOG", preview, geometry.logWidth, geometry.logHeight),
		)
	default:
		project := ""
		if projectIndex >= 0 && projectIndex < len(snapshot.Projects) {
			project = " · " + strings.ToUpper(snapshot.Projects[projectIndex].Name)
		}
		processWidth := max(width-paneFrameWidth-2*paneHorizontalPadding, 1)
		processes := renderVisibleLines(currentProcessRows(snapshot, projectIndex, processIndex, processWidth), processIndex, processWidth, max(geometry.processHeight-paneFrameWidth-1, 1))
		body = lipgloss.JoinVertical(lipgloss.Left,
			renderPane("PROCESSES"+project, processes, width, geometry.processHeight),
			renderPane("LIVE LOG", preview, width, geometry.logHeight),
		)
	}
	return fitScreen(lipgloss.JoinVertical(lipgloss.Left, header, body, footerLine), width, height)
}
```

- [ ] **Step 6: Wire preview into Model**

Add `preview logPreview` to `Model`. Initialize it in `New`:

```go
model := Model{
	services: services,
	width: defaultTerminalWidth,
	height: defaultTerminalHeight,
	preview: newLogPreview(1, 1),
}
model.refresh()
model.refreshPreview()
return model
```

Add:

```go
func (m *Model) resizePreview() {
	geometry := dashboardLayout(m.width, m.height)
	m.preview.resize(
		max(geometry.logWidth-paneFrameWidth-2*paneHorizontalPadding, 1),
		geometry.logBodyHeight,
	)
}

func (m *Model) refreshPreview() {
	m.resizePreview()
	project, name, ok := m.selected()
	if !ok {
		m.preview.show("", "", m.services)
		return
	}
	m.preview.show(project, name, m.services)
}
```

Call `resizePreview` on `tea.WindowSizeMsg`. Refresh preview when selection changes after arrow handling. Refresh on matching `logEventMsg` and raw `logstore.Event`. After runtime snapshots replace current snapshot, clamp selection and refresh if selected project/process changed. Render dashboard with:

```go
content := renderDashboard(m.snapshot, m.projectIndex, m.processIndex, m.preview.render(), m.width, m.height)
```

Delete old footer append because dashboard owns root footer.

- [ ] **Step 7: Run dashboard and package tests**

```bash
go test ./internal/tui -run 'TestDashboard|TestOperation|TestProjectAction' -count=1
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit operations dashboard**

```bash
git add internal/tui/dashboard.go internal/tui/styles.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): add operations dashboard"
```

---

### Task 3: Centered Menus and Confirmation Overlays

**Files:**
- Create: `internal/tui/overlay.go`
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `fitScreen` from Task 2 and Lip Gloss v2 `NewCanvas`, `NewLayer`, `Canvas.Compose`.
- Produces: `composeOverlay(background, foreground string, width, height int) string` used by Task 5.

- [ ] **Step 1: Write failing overlay tests**

Add to `internal/tui/model_test.go`:

```go
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
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'r'})
	plain := stripANSI(model.View().Content)
	if !strings.Contains(plain, "CONFIRM RESTART") || !strings.Contains(plain, "[y] Yes") {
		t.Fatalf("confirmation = %q", plain)
	}
}
```

- [ ] **Step 2: Run tests to verify red state**

```bash
go test ./internal/tui -run 'TestProjectMenuRendersCentered|TestOverlayBlocks|TestConfirmationUsesHighest' -count=1
```

Expected: old inline menus fail centering and overlay-copy assertions.

- [ ] **Step 3: Add overlay composition and box renderers**

Create `internal/tui/overlay.go`:

```go
package tui

import lipgloss "charm.land/lipgloss/v2"

func composeOverlay(background, foreground string, width, height int) string {
	width, height = max(width, 1), max(height, 1)
	background = fitScreen(background, width, height)
	foreground = lipgloss.NewStyle().MaxWidth(width).MaxHeight(height).Render(foreground)
	x := max((width-lipgloss.Width(foreground))/2, 0)
	y := max((height-lipgloss.Height(foreground))/2, 0)
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewLayer(background).Z(0))
	canvas.Compose(lipgloss.NewLayer(foreground).X(x).Y(y).Z(1))
	return fitScreen(canvas.Render(), width, height)
}

func renderProjectMenu() string {
	return overlayStyle.Render(overlayTitleStyle.Render("PROJECT ACTIONS") +
		"\n\n[s] Start  [k] Stop  [r] Restart  [e] Edit\n[Esc] Cancel")
}

func renderAddMenu() string {
	return overlayStyle.Render(overlayTitleStyle.Render("ADD") +
		"\n\n[p] Project  [o] Process\n[Esc] Cancel")
}

func renderConfirmation(current action) string {
	name := "ACTION"
	switch current {
	case stopProcess, stopProject:
		name = "STOP"
	case restartProcess, restartProject:
		name = "RESTART"
	case shutdown:
		name = "SHUTDOWN"
	case saveCritical:
		name = "SAVE AND RESTART"
	}
	return overlayStyle.Render(confirmTitleStyle.Render("CONFIRM "+name) +
		"\n\n[y] Yes  [n/Esc] No")
}

func renderBusy() string {
	return overlayStyle.Render(overlayTitleStyle.Render("WORKING") +
		"\n\nStopping processes…")
}

func renderOperationError(err error) string {
	return overlayStyle.Render(errorStyle.Render("ERROR") + "\n\n" + err.Error())
}
```

Add styles:

```go
overlayStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color(colorAccent)).
	Background(lipgloss.Color(colorSurfaceHigh)).
	Padding(1, 2)
overlayTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(colorAccent))
confirmTitleStyle = overlayTitleStyle.Foreground(lipgloss.Color(colorWarning))
```

- [ ] **Step 4: Apply layers without changing action semantics**

In `Model.View`, build dashboard or full-log base first. Apply layers in this order:

```go
if m.projectMenu {
	content = composeOverlay(content, renderProjectMenu(), m.width, m.height)
}
if m.addMenu {
	content = composeOverlay(content, renderAddMenu(), m.width, m.height)
}
if m.pending != noAction {
	content = composeOverlay(content, renderConfirmation(m.pending), m.width, m.height)
}
if m.busy {
	content = composeOverlay(content, renderBusy(), m.width, m.height)
}
if m.err != nil && m.form == nil {
	content = composeOverlay(content, renderOperationError(m.err), m.width, m.height)
}
```

Return `tea.NewView(fitScreen(content, m.width, m.height))` with `AltScreen = true`. Remove old appended menu, confirmation, busy, error, and footer strings. Keep pending and menu key branches before dashboard key handling. Keep runtime/log/resize cases before key routing.

- [ ] **Step 5: Run overlay and existing action tests**

```bash
go test ./internal/tui -run 'TestProjectMenu|TestOverlay|TestConfirmation|TestStopRequires|TestQuitConfirms|TestOperationErrorRemainsVisible' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit overlays**

```bash
git add internal/tui/overlay.go internal/tui/styles.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): render action overlays"
```

---

### Task 4: Web-Like Controls and Cursor-Safe Editing

**Files:**
- Modify: `internal/tui/form.go`
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/form_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**
- Consumes: existing `formField`, `formToggle`, `textinput.Model`, and config parsers.
- Produces: `isEnumField`, typed `formFieldError`, `editForm.fieldErrors`, square input/select/switch rendering.

- [ ] **Step 1: Write failing cursor and navigation tests**

Add to `internal/tui/form_test.go`:

```go
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
```

- [ ] **Step 2: Write failing field-error tests**

```go
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
```

- [ ] **Step 3: Run tests to verify red state**

```bash
go test ./internal/tui -run 'TestTextArrows|TestTextHomeEnd|TestUpDownNavigate|TestFormAttaches|TestFormKeepsCrossField' -count=1
```

Expected: cursor test fails because global left/right handling swallows text input; error tests fail because errors are summary-only.

- [ ] **Step 4: Route keys by focused control type**

Add:

```go
func isEnumField(label string) bool {
	return label == fieldHealthType || label == fieldRestartPolicy
}
```

In `editForm.update`, handle `Up`, `Down`, `Tab`, and `Shift+Tab` as focus movement. Handle `Left`/`Right` only for enum fields:

```go
label := f.focusLabel()
switch key.Code {
case tea.KeyLeft:
	if isEnumField(label) {
		f.cycleEnum(-1)
		return nil
	}
case tea.KeyRight:
	if isEnumField(label) {
		f.cycleEnum(1)
		return nil
	}
}
```

Do not return for text `Left`, `Right`, `Home`, `End`, `Backspace`, `Delete`, `Ctrl+Left`, or `Ctrl+Right`; dispatch them to focused `textinput.Model.Update`. Bubbles v2.1.1 already binds word navigation to `ctrl+left` and `ctrl+right`.

Before dispatching text input, clear stale errors for current field:

```go
delete(f.fieldErrors, label)
f.err = nil
```

- [ ] **Step 5: Classify parse errors without changing config semantics**

Import `errors`. Add to `editForm` and initialize in both constructors:

```go
fieldErrors map[string]error
```

Add:

```go
type formFieldError struct {
	label string
	err   error
}

func (e formFieldError) Error() string { return e.err.Error() }
func (e formFieldError) Unwrap() error { return e.err }

func fieldFailure(label string, err error) error {
	return formFieldError{label: label, err: err}
}

func (f *editForm) clearErrors() {
	clear(f.fieldErrors)
	f.err = nil
}
```

Wrap every parse failure in `configWithoutValidation` using this exhaustive map:

| Parse target | Form label |
|---|---|
| args JSON | `fieldArgs` |
| health timeout | `HealthTimeout` |
| health interval | `HealthInterval` |
| restart max attempts | `RestartMaxAttempts` |
| restart window | `RestartWindow` |
| initial backoff | `InitialBackoff` |
| max backoff | `MaxBackoff` |
| log max size | `LogMaxSizeMB` |
| log max files | `LogMaxFiles` |
| log buffer lines | `LogBufferLines` |
| stop timeout | `StopTimeout` |

Use this exact pattern at each call:

```go
if err := parseJSONField("args", f.value(fieldArgs), &item.Args); err != nil {
	return config.Config{}, fieldFailure(fieldArgs, err)
}
```

Make `config` own display errors:

```go
func (f *editForm) config() (config.Config, error) {
	f.clearErrors()
	result, err := f.configWithoutValidation()
	if err != nil {
		var fieldErr formFieldError
		if errors.As(err, &fieldErr) {
			f.fieldErrors[fieldErr.label] = fieldErr.err
		} else {
			f.err = err
		}
		return config.Config{}, err
	}
	if err := result.Validate(); err != nil {
		f.err = err
		return config.Config{}, err
	}
	return result, nil
}
```

In `Model.Update`, stop assigning every save error to `m.form.err`; `form.config()` now places it. Keep `m.err = err` for model state.

- [ ] **Step 6: Render square web controls and inline errors**

Replace rounded input/toggle styles with:

```go
formInputStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color(colorMuted)).Padding(0, 1)
formFocusedInputStyle = formInputStyle.BorderForeground(lipgloss.Color(colorAccent))
formInlineErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
formSwitchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorMuted))
formEnabledSwitchStyle = formSwitchStyle.Foreground(lipgloss.Color(colorSuccess)).Bold(true)
```

Keep text input copies width-limited so long values scroll horizontally. Render enum as `SELECT  ‹ value ›`. Render toggle as `[ OFF ] Label` or `[ ON  ] Label`. Append inline error in `renderField`:

```go
if err := f.fieldErrors[field.label]; err != nil {
	result += "\n" + formInlineErrorStyle.Render(err.Error())
}
```

For environment rows, render sorted `KEY  ••••  Ctrl+X delete`; never render values.

- [ ] **Step 7: Run all form and security tests**

```bash
go test ./internal/tui -run 'Test.*Form|TestShiftTab|TestActiveProcess|TestCriticalEdit' -count=1
```

Expected: PASS, including existing secret-masking tests.

- [ ] **Step 8: Commit form controls**

```bash
git add internal/tui/form.go internal/tui/styles.go internal/tui/form_test.go internal/tui/model.go
git commit -m "feat(tui): add web-like form controls"
```

---

### Task 5: Scrollable Form Modal

**Files:**
- Modify: `internal/tui/form.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/form_test.go`
- Modify: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `composeOverlay` from Task 3 and controls from Task 4.
- Produces: `modalSize(terminalWidth, terminalHeight int) (width, height int)`, `editForm.body viewport.Model`, focused-control viewport synchronization.

- [ ] **Step 1: Write failing modal geometry and viewport tests**

Add to `internal/tui/form_test.go`:

```go
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
```

Add to `internal/tui/model_test.go`:

```go
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
	if !strings.Contains(plain, "PROJECTS") || !strings.Contains(plain, "Edit project · shop") {
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

func TestMatchingLogEventRefreshesPreviewBehindForm(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Projects = []config.Project{{Name: "shop", Directory: dir, Processes: []config.Process{{Name: "api", Command: "api"}}}}
	records := []logstore.Record{{At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "before"}}
	model := tui.New(tui.Services{
		Snapshots: dashboardFixture,
		Config: func() config.Config { return cfg },
		LogSnapshot: func(string, string) []logstore.Record { return records },
	})
	model = updateModel(t, model, tea.KeyPressMsg{Code: 'e'})
	records = []logstore.Record{{At: time.Unix(2, 0), Stream: logstore.Stdout, Text: "after"}}
	model, _ = update(model, logstore.Event{Project: "shop", Process: "api", Records: records})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !strings.Contains(model.View().Content, "after") {
		t.Fatalf("preview did not refresh behind form: %q", model.View().Content)
	}
}
```

Add `time` and `runp/internal/logstore` imports if Task 2 did not already add them.

- [ ] **Step 2: Run tests to verify red state**

```bash
go test ./internal/tui -run 'TestModalSize|TestFormViewport|TestFormRendersAsModal|TestDashboardInputBlockedWhileFormOpen|TestMatchingLogEventRefreshesPreviewBehindForm' -count=1
```

Expected: missing `modalSize`, clipped content, and dashboard absent behind form.

- [ ] **Step 3: Add modal dimensions and body viewport**

Import Bubbles viewport in `form.go`. Add:

```go
const (
	modalMaxWidth      = 90
	modalMinimumWidth  = 60
	modalMinimumHeight = 16
	modalMargin        = 4
)

func modalSize(terminalWidth, terminalHeight int) (int, int) {
	terminalWidth, terminalHeight = max(terminalWidth, 1), max(terminalHeight, 1)
	if terminalWidth < modalMinimumWidth || terminalHeight < modalMinimumHeight {
		return terminalWidth, terminalHeight
	}
	return min(modalMaxWidth, terminalWidth-modalMargin), terminalHeight-modalMargin
}
```

Add `body viewport.Model` to `editForm`. Initialize in both constructors:

```go
body := viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
body.FillHeight = true
```

Assign `body: body` in form literals.

- [ ] **Step 4: Render controls with exact line ranges**

Add:

```go
type controlRange struct{ start, end int }

func (f *editForm) panelContent(width int) (string, map[string]controlRange) {
	parts := make([]string, 0)
	ranges := make(map[string]controlRange)
	line := 0
	active := f.activeSection()
	for _, field := range f.fields {
		if field.section != active {
			continue
		}
		rendered := f.renderField(field, width)
		height := lipgloss.Height(rendered)
		ranges[field.label] = controlRange{start: line, end: line + height - 1}
		parts = append(parts, rendered)
		line += height + 1
	}
	for _, toggle := range f.toggles {
		if toggle.section != active {
			continue
		}
		rendered := f.renderToggle(toggle)
		ranges[toggle.label] = controlRange{start: line, end: line}
		parts = append(parts, rendered)
		line += 2
	}
	return strings.Join(parts, "\n\n"), ranges
}

func (f *editForm) syncBody(width, height int) {
	content, ranges := f.panelContent(width)
	f.body.SetWidth(max(width, 1))
	f.body.SetHeight(max(height, 1))
	f.body.SetContent(content)
	focused, ok := ranges[f.focusLabel()]
	if !ok {
		return
	}
	offset := f.body.YOffset()
	if focused.start < offset {
		offset = focused.start
	} else if focused.end >= offset+f.body.Height() {
		offset = focused.end - f.body.Height() + 1
	}
	f.body.SetYOffset(max(offset, 0))
}
```

Call `syncBody` after focus movement and before render. Resize inputs and viewport from modal inner dimensions, not terminal dimensions.

- [ ] **Step 5: Render fixed modal chrome around viewport**

Add `formModalStyle` with normal border, `colorSurfaceHigh` background, and no outer padding. Refactor `editForm.view`:

1. Calculate `modalWidth, modalHeight := modalSize(f.width, f.height)`.
2. Reserve two rows for border, one header row, one footer row, one section/tab row for process forms, and summary error height only when `f.err != nil`.
3. Give all remaining rows to `f.body`.
4. At modal width `>= wideFormBreakpoint`, render section sidebar beside body; otherwise render tabs above body.
5. Project form omits section row/sidebar.
6. Return exact modal size:

```go
return formModalStyle.
	Width(max(modalWidth-formModalStyle.GetHorizontalFrameSize(), 1)).
	Height(max(modalHeight-formModalStyle.GetVerticalFrameSize(), 1)).
	MaxWidth(modalWidth).
	MaxHeight(modalHeight).
	Render(content)
```

Keep header, inline field errors, summary error, and footer inside fixed chrome. Footer remains `Ctrl+S Save  Esc Cancel`; Environment appends Enter/Ctrl+X hints.

- [ ] **Step 6: Compose form over dashboard and keep routing exclusive**

In `Model.View`, stop returning early for `m.form`. Build dashboard base first, then full log if open, then:

```go
if m.form != nil {
	content = composeOverlay(content, m.form.view(), m.width, m.height)
}
```

Apply menu and confirmation layers afterward. Keep form key branch before dashboard key handling. Runtime/log/resize cases stay before key handling, allowing preview updates behind modal.

- [ ] **Step 7: Run modal, form, action, and secret tests**

```bash
go test ./internal/tui -run 'TestModal|TestFormViewport|TestFormRendersAsModal|TestDashboardInputBlockedWhileFormOpen|TestMatchingLogEventRefreshesPreviewBehindForm|TestProcessForm|TestProjectForm|TestActiveProcess|TestCriticalEdit' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit modal forms**

```bash
git add internal/tui/form.go internal/tui/model.go internal/tui/styles.go internal/tui/form_test.go internal/tui/model_test.go
git commit -m "feat(tui): show forms as modals"
```

---

### Task 6: Operations Full Log Viewer and Final Verification

**Files:**
- Modify: `internal/tui/logview.go`
- Modify: `internal/tui/logview_test.go`
- Modify: `internal/tui/model.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: `fitScreen`, operations styles, shared log formatter.
- Produces: exact-size full log screen retaining existing follow, stream, search, and match behavior.

- [ ] **Step 1: Write failing exact-size log viewer tests**

Add Lip Gloss import and these tests to `internal/tui/logview_test.go`:

```go
func TestLogViewerUsesOperationsChromeAndWholeTerminal(t *testing.T) {
	model := logModelWithRecords([]logstore.Record{{
		At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "ready",
	}})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 90, Height: 22})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	view := model.View().Content
	if width, height := lipgloss.Size(view); width != 90 || height != 22 {
		t.Fatalf("screen = %dx%d", width, height)
	}
	plain := stripANSI(view)
	for _, want := range []string{"RUNP", "SHOP / API", "BOTH", "FOLLOW", "ready", "[f] Follow", "[/] Search"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("view missing %q: %q", want, plain)
		}
	}
}

func TestLogSearchKeepsWholeTerminal(t *testing.T) {
	model := logModelWithRecords(nil)
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 60, Height: 16})
	model = updateModel(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updateModel(t, model, tea.KeyPressMsg{Code: '/'})
	if width, height := lipgloss.Size(model.View().Content); width != 60 || height != 16 {
		t.Fatalf("search screen = %dx%d", width, height)
	}
}
```

- [ ] **Step 2: Run tests to verify red state**

```bash
go test ./internal/tui -run 'TestLogViewerUsesOperationsChrome|TestLogSearchKeepsWholeTerminal' -count=1
```

Expected: old viewer fails operations chrome and exact-size assertions.

- [ ] **Step 3: Restyle full viewer without changing update behavior**

Add `width` and `height` fields to `logView`; set them in constructor and `resize`. Change `logHeaderFooterHeight` to 2. Render:

```go
func (l logView) render() string {
	stream := "BOTH"
	if l.stream == logstore.Stdout {
		stream = "STDOUT"
	} else if l.stream == logstore.Stderr {
		stream = "STDERR"
	}
	mode := "PAUSED"
	if l.follow {
		mode = "FOLLOW"
	}
	header := appHeaderStyle.Width(l.width).MaxHeight(1).Render(fmt.Sprintf(
		"RUNP  %s / %s  %s  %s",
		strings.ToUpper(l.project), strings.ToUpper(l.process), stream, mode,
	))
	if l.query != "" {
		header = appHeaderStyle.Width(l.width).MaxHeight(1).Render(
			fmt.Sprintf("RUNP  %s / %s  %s  %s  /%s", strings.ToUpper(l.project), strings.ToUpper(l.process), stream, mode, l.query),
		)
	}
	footer := appFooterStyle.Width(l.width).MaxHeight(1).Render(
		"[Esc] Back  [f] Follow  [t] Stream  [/] Search  [n/N] Match",
	)
	if l.search {
		footer = appFooterStyle.Width(l.width).MaxHeight(1).Render(l.input.View())
	}
	return fitScreen(
		lipgloss.JoinVertical(lipgloss.Left, header, l.viewport.View(), footer),
		l.width, l.height,
	)
}
```

Set viewport height to `max(height-logHeaderFooterHeight, 1)` and input width to `max(width-searchPromptWidth, 1)` in constructor/resize. Do not change `logView.update`, filtering, highlights, or follow behavior.

- [ ] **Step 4: Run log and full TUI suites**

```bash
go test ./internal/tui -run 'TestLog|TestCtrlCQuitsFromLogViewer' -count=1
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Step 5: Update README**

Under `## Keys`, before dashboard keys, add:

```markdown
+Dashboard fills the terminal with responsive project, process, PID, and selected-process live-log panes. Forms open as keyboard-controlled modal overlays; small terminals use full-screen forms.
```

Keep existing key lists because commands do not change.

- [ ] **Step 6: Run repository verification**

```bash
gofmt -w internal/tui/dashboard.go internal/tui/form.go internal/tui/logview.go internal/tui/model.go internal/tui/overlay.go internal/tui/styles.go internal/tui/form_test.go internal/tui/logview_internal_test.go internal/tui/logview_test.go internal/tui/model_test.go
go test ./...
go vet ./...
git diff --check
git diff -- go.mod go.sum
go test ./internal/tui -run 'TestProcessFormMasksEnvironmentValues|TestTextArrowsEditAtCursorWithoutChangingField|TestDashboardUsesWholeTerminalAndShowsPID' -count=1
```

Expected: all commands exit `0`; dependency diff is empty; security, cursor, and PID tests pass.

- [ ] **Step 7: Review scope and commit final integration**

Run:

```bash
git diff --stat
git status --short
```

Expected: changes limited to planned `internal/tui` files and `README.md`; no config/controller/process/dependency changes. Existing unrelated untracked files remain untouched.

Commit:

```bash
git add internal/tui/logview.go internal/tui/logview_test.go internal/tui/model.go README.md
git commit -m "feat(tui): unify operations screens"
```
