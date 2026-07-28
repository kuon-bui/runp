# Named Constants Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make production Go code easier to read by naming repeated or non-obvious literals without changing behavior.

**Architecture:** Keep constants in package/file owning each meaning; export only config domain values already consumed across package boundaries. Prefer standard-library constants over project aliases. Change no APIs, serialized values, rendering, timing, permissions, or dependencies.

**Tech Stack:** Go 1.26, standard library, Bubble Tea v2, Bubbles v2, Lip Gloss v2

## Global Constraints

- Refactor production Go files across repository; change tests only when compilation or existing assertions require it.
- Preserve behavior, config schema, rendered output, keyboard controls, timeouts, permissions, platform behavior, and public package APIs.
- Add no dependencies, wrapper types, generic `constants.go`, or repository-wide constants package.
- Do not extract zero-value checks, loop increments, slice indexes, one-off punctuation, or other self-explanatory literals.
- Use purpose names, not value names: `wideFormBreakpoint`, not `width80`.
- Keep string-backed config fields; dedicated domain types remain deferred.
- Existing tests are behavior lock; constant substitutions need no new tests.

---

### Task 1: Configuration Domain Constants

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/env.go`
- Modify: `internal/config/store.go`
- Test: `internal/config/config_test.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Produces: exported string constants `HealthProcess`, `HealthHTTP`, `HealthTCP`, `RestartNever`, `RestartOnFailure`, and `RestartAlways` for process and TUI packages.
- Produces: package constants for config version, defaults, TCP port bounds, scanner limits, and permissions.
- Consumes: no new interfaces.

- [ ] **Step 1: Record baseline config behavior**

Run: `go test ./internal/config`

Expected: PASS. No new test needed because changes only replace literals and existing tests cover defaults, validation, resolution, env parsing, and secure persistence.

- [ ] **Step 2: Name config domain values and defaults**

In `internal/config/config.go`, add constants near `type Duration`:

```go
const (
	currentVersion = 1

	HealthProcess = "process"
	HealthHTTP    = "http"
	HealthTCP     = "tcp"

	RestartNever     = "never"
	RestartOnFailure = "on-failure"
	RestartAlways    = "always"

	defaultStopTimeout  = 5 * time.Second
	defaultLogMaxSizeMB = 10
	defaultLogMaxFiles  = 5
	defaultLogBuffer    = 10_000

	defaultRestartMaxAttempts = 5
	defaultRestartWindow      = time.Minute
	defaultInitialBackoff     = time.Second
	defaultMaxBackoff         = 30 * time.Second

	defaultHealthTimeout  = 30 * time.Second
	defaultHealthInterval = 500 * time.Millisecond

	minimumTCPPort = 1
	maximumTCPPort = 65_535
)
```

Replace corresponding literals in `Default`, `Validate`, `validateHealth`, `validateRestart`, and `Resolve`. Keep exact values and error text. Use grouped switch values where clearer:

```go
switch typeName {
case HealthProcess:
case HealthHTTP:
case HealthTCP:
}
```

Use policy constants in validation:

```go
if policy != RestartNever && policy != RestartOnFailure && policy != RestartAlways {
```

- [ ] **Step 3: Name env scanner limits and storage permissions**

In `internal/config/env.go`, add near `envKeyPattern`:

```go
const (
	initialEnvScanBuffer = 4 * 1024
	maximumEnvLineBytes  = 1 << 20
)
```

Replace matching `scanner.Buffer` arguments without changing limits.

In `internal/config/store.go`, add near imports:

```go
const (
	configDirectoryMode = 0o700
	configFileMode      = 0o600
)
```

Replace `os.MkdirAll(directory, 0o700)` and `temporary.Chmod(0o600)`. Do not alias JSON indentation or newline because both are local and self-explanatory.

- [ ] **Step 4: Format and verify config package**

Run: `gofmt -w internal/config/config.go internal/config/env.go internal/config/store.go && go test ./internal/config`

Expected: PASS.

- [ ] **Step 5: Commit config constants**

Run:

```bash
git add internal/config/config.go internal/config/env.go internal/config/store.go
git commit -m "refactor: name config domain constants"
```

---

### Task 2: Process Runtime Constants

**Files:**
- Modify: `internal/process/command.go`
- Modify: `internal/process/health.go`
- Modify: `internal/process/restart.go`
- Modify: `internal/process/manager.go`
- Test: `internal/process/command_test.go`
- Test: `internal/process/health_test.go`
- Test: `internal/process/restart_test.go`
- Test: `internal/process/manager_test.go`

**Interfaces:**
- Consumes: `config.HealthProcess`, `config.HealthHTTP`, `config.HealthTCP`, `config.RestartNever`, `config.RestartOnFailure`, and `config.RestartAlways` from Task 1.
- Produces: no new exported API.

- [ ] **Step 1: Record baseline process behavior**

Run: `go test ./internal/process`

Expected: PASS.

- [ ] **Step 2: Reuse config domain constants**

In `internal/process/health.go`, replace health type literals with exported config constants:

```go
if cfg.Type == config.HealthProcess {
```

```go
case config.HealthHTTP:
	probeErr = probeHTTP(ctx, cfg.URL)
case config.HealthTCP:
	probeErr = probeTCP(ctx, cfg.Address)
```

Use HTTP status constants for successful range:

```go
if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
```

Keep network string `"tcp"` inline because it belongs to `net.Dialer`, not config domain vocabulary.

In `internal/process/restart.go`, replace policy literals:

```go
if expected || policy == config.RestartNever || policy == config.RestartOnFailure && exitCode == 0 {
```

```go
if policy != config.RestartAlways && policy != config.RestartOnFailure {
```

- [ ] **Step 3: Name process command and exit sentinels**

In `internal/process/command.go`, add local shell constants:

```go
const (
	windowsShell        = "cmd.exe"
	windowsShellCommand = "/C"
	unixShell           = "/bin/sh"
	unixShellCommand    = "-c"
)
```

Replace command literals without changing argument order.

In `internal/process/manager.go`, add near state constants:

```go
const (
	managerEventBuffer = 64
	unknownExitCode    = -1
)
```

Replace manager event channel capacity and both `-1` sentinel returns/assignments. Keep notifier channel capacity `1` inline because single-wakeup semantics are obvious beside channel construction.

- [ ] **Step 4: Format and verify process package**

Run: `gofmt -w internal/process/command.go internal/process/health.go internal/process/restart.go internal/process/manager.go && go test ./internal/process`

Expected: PASS.

- [ ] **Step 5: Commit process constants**

Run:

```bash
git add internal/process/command.go internal/process/health.go internal/process/restart.go internal/process/manager.go
git commit -m "refactor: name process runtime constants"
```

---

### Task 3: App, Controller, Log, and Platform Limits

**Files:**
- Modify: `internal/app/run.go`
- Modify: `internal/app/signals.go`
- Modify: `internal/controller/controller.go`
- Modify: `internal/logstore/logstore.go`
- Modify: `internal/logstore/rotate.go`
- Modify: `internal/procgroup/group_windows.go`
- Test: existing tests under matching packages

**Interfaces:**
- Consumes: existing constructors and behavior only.
- Produces: no new exported API.

- [ ] **Step 1: Record baseline package behavior**

Run: `go test ./internal/app ./internal/controller ./internal/logstore ./internal/procgroup`

Expected: PASS.

- [ ] **Step 2: Name app timing and signal limits**

In `internal/app/run.go`, add:

```go
const (
	logBatchInterval = 50 * time.Millisecond
	shutdownTimeout  = 10 * time.Second
)
```

Replace matching constructor and cleanup literals.

In `internal/app/signals.go`, add:

```go
const (
	signalBuffer         = 2
	forceShutdownTimeout = 5 * time.Second
)
```

Replace matching channel and timeout literals.

- [ ] **Step 3: Name controller and log operational limits**

In `internal/controller/controller.go`, add:

```go
const controllerEventBuffer = 64
```

Replace event channel capacity.

In `internal/logstore/logstore.go`, retain existing `maxRecordBytes` and add:

```go
const (
	defaultBatchInterval = 50 * time.Millisecond
	storeEventBuffer     = 64
	logDirectoryMode     = 0o700
	bytesPerMegabyte     = 1 << 20
)
```

Replace matching literals in `New` and `Open`. Use `bytesPerMegabyte` for size conversion:

```go
sink, err := newRotatingWriter(path, int64(cfg.MaxSizeMB)*bytesPerMegabyte, cfg.MaxFiles)
```

In `internal/logstore/rotate.go`, add and use:

```go
const logFileMode = 0o600
```

Do not name `maxFiles == 1`; it directly communicates single-file rotation behavior.

- [ ] **Step 4: Name Windows forced-exit code**

In `internal/procgroup/group_windows.go`, add:

```go
const forcedExitCode = 1
```

Use it in `windows.TerminateJobObject`. Keep Unix signals as standard-library constants.

- [ ] **Step 5: Format and verify affected packages**

Run: `gofmt -w internal/app/run.go internal/app/signals.go internal/controller/controller.go internal/logstore/logstore.go internal/logstore/rotate.go internal/procgroup/group_windows.go && go test ./internal/app ./internal/controller ./internal/logstore ./internal/procgroup`

Expected: PASS on current OS; Windows file must remain syntactically formatted and later cross-compile check covers it.

- [ ] **Step 6: Commit operational constants**

Run:

```bash
git add internal/app/run.go internal/app/signals.go internal/controller/controller.go internal/logstore/logstore.go internal/logstore/rotate.go internal/procgroup/group_windows.go
git commit -m "refactor: name operational limits"
```

---

### Task 4: TUI Palette and Layout Constants

**Files:**
- Modify: `internal/tui/styles.go`
- Modify: `internal/tui/dashboard.go`
- Modify: `internal/tui/logview.go`
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`
- Test: `internal/tui/logview_test.go`

**Interfaces:**
- Produces: package constants reused by TUI files for dimensions, breakpoints, spacing, and colors.
- Consumes: existing Lip Gloss and Bubble Tea APIs.

- [ ] **Step 1: Record baseline TUI behavior**

Run: `go test ./internal/tui`

Expected: PASS.

- [ ] **Step 2: Name palette and shared dimensions**

In `internal/tui/styles.go`, add above style variables:

```go
const (
	colorAccent  = "12"
	colorMuted   = "8"
	colorSuccess = "10"
	colorError   = "9"
	colorWarning = "11"
	colorSurface = "236"

	defaultTerminalWidth  = 80
	defaultTerminalHeight = 24
	wideFormBreakpoint       = 80
	wideDashboardBreakpoint  = 90
	compactFooterBreakpoint = 70

	cardHorizontalPadding = 1
	dashboardHorizontalInset = 4
	dashboardTwoColumnInset  = 6
	cardFrameWidth           = 4
	panelGap                 = 2
	formOuterInset           = 4
	formInnerInset           = 4
	formSidebarWidth         = 16
	minimumPanelWidth        = 24
)
```

Align names with `gofmt`; exact declaration spacing need not match snippet. Replace every `lipgloss.Color("...")` in `styles.go` with named palette constants. Replace focused toggle accent color in `form.go` during Task 5.

- [ ] **Step 3: Replace layout literals at use sites**

In `internal/tui/dashboard.go`, use `wideDashboardBreakpoint`, `compactFooterBreakpoint`, `dashboardHorizontalInset`, `dashboardTwoColumnInset`, `cardFrameWidth`, and `panelGap`. Preserve exact width arithmetic and rendered two-space gap.

In `internal/tui/model.go`, initialize with `defaultTerminalWidth` and `defaultTerminalHeight`.

In `internal/tui/logview.go`, add local structural constants:

```go
const (
	logHeaderFooterHeight = 3
	searchPromptWidth     = 2
	logTimeFormat         = "15:04:05.000"
)
```

Replace matching repeated height/width offsets and time format. Do not extract display strings `"OUT"`, `"ERR"`, `"FOLLOW"`, or footer text because each is local presentation vocabulary and already readable.

- [ ] **Step 4: Format and verify TUI package**

Run: `gofmt -w internal/tui/styles.go internal/tui/dashboard.go internal/tui/logview.go internal/tui/model.go && go test ./internal/tui`

Expected: PASS, proving output and dimensions remain unchanged.

- [ ] **Step 5: Commit TUI layout constants**

Run:

```bash
git add internal/tui/styles.go internal/tui/dashboard.go internal/tui/logview.go internal/tui/model.go
git commit -m "refactor: name tui layout constants"
```

---

### Task 5: TUI Form Identifier Constants

**Files:**
- Modify: `internal/tui/form.go`
- Test: `internal/tui/form_test.go`

**Interfaces:**
- Consumes: palette/layout constants from Task 4 and config domain constants from Task 1.
- Produces: no exported API; internal field IDs remain exact string values used by form lookup and config conversion.

- [ ] **Step 1: Define repeated field and toggle identifiers**

Near `formKind` in `internal/tui/form.go`, define only identifiers used in multiple methods or behavior branches:

```go
const (
	fieldName          = "Name"
	fieldDirectory     = "Directory"
	fieldArgs          = "Args"
	fieldEnvKey        = "EnvKey"
	fieldEnvValue      = "EnvValue"
	fieldHealthType    = "HealthType"
	fieldRestartPolicy = "RestartPolicy"

	toggleShell     = "Shell"
	toggleAutostart = "Autostart"
)
```

Keep one-use mapping identifiers such as `"HealthURL"`, `"LogMaxFiles"`, and `"StopTimeout"` inline; extracting them would add indirection without removing drift risk.

- [ ] **Step 2: Replace repeated IDs and domain values**

Replace matching lookup, comparison, map-key, and form-construction strings throughout `form.go`. Use config domain constants in `cycleEnum`:

```go
case fieldHealthType:
	values = []string{config.HealthProcess, config.HealthHTTP, config.HealthTCP}
case fieldRestartPolicy:
	values = []string{config.RestartNever, config.RestartOnFailure, config.RestartAlways}
```

Use `colorAccent` from Task 4 in `renderToggle`.

Replace hard-coded form dimensions and layout values with Task 4 constants, preserving formulas:

```go
width:  defaultTerminalWidth,
height: defaultTerminalHeight,
```

```go
if f.kind == processForm && f.width >= wideFormBreakpoint {
	sidebar := f.renderSidebar(formSidebarWidth)
	panelWidth := max(bodyWidth-lipgloss.Width(sidebar)-panelGap, minimumPanelWidth)
```

Keep tiny-terminal guard `9` inline unless naming it clearly improves its relation to Lip Gloss frame overhead; do not force extraction.

- [ ] **Step 3: Format and verify form behavior**

Run: `gofmt -w internal/tui/form.go && go test ./internal/tui`

Expected: PASS, including secret masking, focus movement, responsive rendering, and config round trips.

- [ ] **Step 4: Commit form identifiers**

Run:

```bash
git add internal/tui/form.go
git commit -m "refactor: name tui form identifiers"
```

---

### Task 6: Repository Verification and Cleanup

**Files:**
- Inspect: all modified production Go files
- Inspect: `docs/superpowers/specs/2026-07-28-named-constants-refactor-design.md`
- Preserve: unrelated untracked `docs/superpowers/specs/2026-07-28-semantic-constants-refactor-design.md`

**Interfaces:**
- Consumes: all prior task changes.
- Produces: behavior-neutral, formatted, cross-platform-valid repository.

- [ ] **Step 1: Review diff for over-extraction and behavior drift**

Run: `git diff HEAD~5 -- internal '*.go'`

Expected: substitutions and declarations only. Remove aliases for trivial local values. Confirm no string value, timeout, capacity, permission, status boundary, layout formula, or command argument changed.

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`

Expected: PASS for every package.

- [ ] **Step 3: Verify Windows compilation**

Run: `GOOS=windows GOARCH=amd64 go test ./internal/config ./internal/process ./internal/procgroup ./internal/app`

Expected: packages compile and tests not requiring Unix execution pass. If runtime-only tests cannot execute cross-built binaries, use compile-only commands for affected packages:

```bash
GOOS=windows GOARCH=amd64 go test -run '^$' ./internal/config ./internal/process ./internal/procgroup ./internal/app
```

Expected: PASS.

- [ ] **Step 4: Check workspace diagnostics**

Inspect diagnostics for all changed Go files. Expected: no new compile, type, or lint errors.

- [ ] **Step 5: Confirm clean intended state**

Run: `git status --short --branch`

Expected: only intentional commits plus pre-existing untracked `docs/superpowers/specs/2026-07-28-semantic-constants-refactor-design.md`. Do not stage or delete that unrelated file.

- [ ] **Step 6: Commit cleanup only if needed**

If review or formatting required changes, run:

```bash
git add internal
git commit -m "refactor: finish named constants cleanup"
```

If no tracked changes remain, skip commit.
