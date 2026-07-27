# `runp` Process Manager TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `runp`, a cross-platform Bubble Tea TUI that configures, starts, logs, stops, and restarts development-project process trees.

**Architecture:** One foreground binary owns all child processes. Config, log capture, OS process groups, process lifecycle, dependency controller, and Bubble Tea UI have narrow package boundaries; TUI consumes copied snapshots and typed events rather than shared mutable state.

**Tech Stack:** Go 1.26.1; Bubble Tea `charm.land/bubbletea/v2@v2.0.8`; Bubbles `charm.land/bubbles/v2@v2.1.1`; Lip Gloss `charm.land/lipgloss/v2@v2.0.5`; `golang.org/x/sys@v0.47.0`; Go standard library.

## Global Constraints

- Binary name: `runp`; current module path remains `run-project`.
- Supported OS: Linux, macOS, Windows.
- One foreground process; no daemon, remote control, PTY, or child stdin attachment.
- Confirmed quit stops and reaps every process tree owned by current `runp` session.
- Default config: `<os.UserConfigDir()>/runp/config.json`; `--config <path>` overrides it.
- Default runtime data: `<os.UserCacheDir()>/runp/`; logs rotate at 10 MiB and keep 5 files total.
- Config format version is exactly `1`; writes use same-directory temporary file, sync, atomic replacement, and directory sync where supported.
- Direct commands execute executable plus args; shell commands use `/bin/sh -c` on Unix and `cmd.exe /C` on Windows.
- Dependencies stay within one project and must form an acyclic graph.
- Health types: `process`, `http`, `tcp`; restart policies: `never`, `on-failure`, `always`.
- Child environment merge order: parent environment, `envFile`, explicit `env`.
- State and focus always have text/non-color cues; all actions remain keyboard accessible.
- Non-trivial behavior follows red-green-refactor TDD; run race tests on Linux after normal tests.

## Execution Preflight

Current workspace has no Git repository. Before feature work, create `.gitignore` containing:

```gitignore
.superpowers/
runp
runp.exe
coverage.out
```

Then initialize baseline history:

```bash
git init
git add .gitignore go.mod main.go docs/superpowers/specs/2026-07-27-runp-design.md docs/superpowers/plans/2026-07-27-runp-implementation.md
git commit -m "chore: initialize runp project"
```

After baseline commit, use `superpowers:using-git-worktrees` before executing Task 1. If user explicitly declines Git initialization, work in current directory and omit only commit/worktree steps; product steps and verification stay unchanged.

## Planned File Map

```text
.gitignore                                      generated in preflight
.github/workflows/ci.yml                        cross-platform test/build matrix
README.md                                       install, config, keys, behavior
main.go                                         tiny executable entry point
go.mod                                          exact dependency versions
go.sum                                          resolved dependency checksums
internal/config/config.go                       schema, defaults, validation, resolution
internal/config/env.go                          non-executing .env parser
internal/config/store.go                        paths, load, atomic save
internal/config/replace_unix.go                  Unix atomic replace and directory sync
internal/config/replace_windows.go               Windows MoveFileEx replacement
internal/config/config_test.go                   schema/default/graph/resolution tests
internal/config/env_test.go                      .env parser tests
internal/config/store_test.go                    creation and preservation tests
internal/logstore/logstore.go                    records, ring, stream writers, event batches
internal/logstore/rotate.go                      bounded file rotation
internal/logstore/logstore_test.go               stream, chunk, filter, batch tests
internal/logstore/rotate_test.go                 rotation and retry tests
internal/process/health.go                       process/HTTP/TCP readiness
internal/process/restart.go                      rolling restart budget and backoff
internal/process/health_test.go                  readiness tests
internal/process/restart_test.go                 policy tests
internal/procgroup/group_unix.go                 Unix process-group lifecycle
internal/procgroup/group_windows.go              Windows Job Object lifecycle
internal/procgroup/group_unix_test.go            Unix descendant cleanup integration test
internal/process/manager.go                      per-process state machine and ownership
internal/process/command.go                      command/env construction
internal/process/manager_test.go                 lifecycle and process-helper tests
internal/controller/graph.go                     dependency graph operations
internal/controller/controller.go                project/group orchestration and snapshots
internal/controller/controller_test.go           dependency and autostart integration tests
internal/tui/model.go                            screen state, messages, service callbacks
internal/tui/dashboard.go                        responsive project-card rendering
internal/tui/styles.go                           accessible Lip Gloss styles
internal/tui/model_test.go                       dashboard navigation/action tests
internal/tui/logview.go                          viewport, follow, search, stream filter
internal/tui/logview_test.go                     log interaction tests
internal/tui/form.go                             project/process forms and safe config edits
internal/tui/form_test.go                        validation and secret-rendering tests
internal/app/run.go                              wiring, flags, signals, shutdown, panic boundary
internal/app/signals_unix.go                     Unix interrupt and SIGTERM registration
internal/app/signals_windows.go                  Windows interrupt registration
internal/app/run_test.go                         flag/path/error/shutdown tests
internal/app/integration_test.go                 full helper-process workflow
```

---

### Task 1: Configuration Model and Atomic Store

**Files:**
- Modify: `go.mod`
- Create: `go.sum`
- Create: `internal/config/config.go`
- Create: `internal/config/env.go`
- Create: `internal/config/store.go`
- Create: `internal/config/replace_unix.go`
- Create: `internal/config/replace_windows.go`
- Test: `internal/config/config_test.go`
- Test: `internal/config/env_test.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: Go standard library; `golang.org/x/sys/windows` only in Windows replacement file.
- Produces:

```go
type Duration time.Duration

type Config struct {
    Version  int      `json:"version"`
    Defaults Defaults `json:"defaults"`
    Projects []Project `json:"projects"`
}

type Defaults struct {
    StopTimeout Duration      `json:"stopTimeout"`
    Log         LogConfig     `json:"log"`
    Restart     RestartConfig `json:"restart"`
}

type LogConfig struct {
    MaxSizeMB  int `json:"maxSizeMB"`
    MaxFiles   int `json:"maxFiles"`
    BufferLines int `json:"bufferLines"`
}

type RestartConfig struct {
    Policy         string   `json:"policy,omitempty"`
    MaxAttempts    int      `json:"maxAttempts,omitempty"`
    Window         Duration `json:"window,omitempty"`
    InitialBackoff Duration `json:"initialBackoff,omitempty"`
    MaxBackoff     Duration `json:"maxBackoff,omitempty"`
}

type HealthConfig struct {
    Type     string   `json:"type,omitempty"`
    URL      string   `json:"url,omitempty"`
    Address  string   `json:"address,omitempty"`
    Timeout  Duration `json:"timeout,omitempty"`
    Interval Duration `json:"interval,omitempty"`
}

type Project struct {
    Name      string    `json:"name"`
    Directory string    `json:"directory"`
    Autostart bool      `json:"autostart"`
    Processes []Process `json:"processes"`
}

type Process struct {
    Name       string            `json:"name"`
    Command    string            `json:"command"`
    Args       []string          `json:"args,omitempty"`
    Shell      bool              `json:"shell"`
    Directory  string            `json:"directory,omitempty"`
    Env        map[string]string `json:"env,omitempty"`
    EnvFile    string            `json:"envFile,omitempty"`
    Autostart  bool              `json:"autostart"`
    StopTimeout Duration         `json:"stopTimeout,omitempty"`
    DependsOn  []string          `json:"dependsOn,omitempty"`
    Health     HealthConfig      `json:"health,omitempty"`
    Restart    RestartConfig     `json:"restart,omitempty"`
    Log        LogConfig         `json:"log,omitempty"`
}

type ResolvedProcess struct {
    ProjectName string
    Name        string
    Directory   string
    Command     string
    Args        []string
    Shell       bool
    Env         []string
    DependsOn   []string
    Autostart   bool
    StopTimeout time.Duration
    Health      HealthConfig
    Restart     RestartConfig
    Log         LogConfig
}

func Default() Config
func DefaultPath() (string, error)
func DataDir() (string, error)
func Load(path string) (Config, error)
func Save(path string, cfg Config) error
func ParseEnv(r io.Reader) (map[string]string, error)
func SafeName(name string) string
func (c Config) Validate() error
func (c Config) Resolve(projectName, processName string) (ResolvedProcess, error)
```

- [ ] **Step 1: Pin dependencies and write failing duration/default tests**

Add `golang.org/x/sys v0.47.0` to `go.mod`, then add tests proving string-duration JSON and exact defaults:

```go
func TestDurationJSON(t *testing.T) {
    var got struct {
        Timeout config.Duration `json:"timeout"`
    }
    if err := json.Unmarshal([]byte(`{"timeout":"5s"}`), &got); err != nil {
        t.Fatal(err)
    }
    if time.Duration(got.Timeout) != 5*time.Second {
        t.Fatalf("timeout = %s", time.Duration(got.Timeout))
    }
}

func TestDefault(t *testing.T) {
    got := config.Default()
    if got.Version != 1 || time.Duration(got.Defaults.StopTimeout) != 5*time.Second {
        t.Fatalf("unexpected defaults: %#v", got)
    }
    if got.Defaults.Log != (config.LogConfig{MaxSizeMB: 10, MaxFiles: 5, BufferLines: 10000}) {
        t.Fatalf("log defaults = %#v", got.Defaults.Log)
    }
}
```

- [ ] **Step 2: Run config tests and verify red state**

```bash
go test ./internal/config -run 'TestDurationJSON|TestDefault' -v
```

Expected: FAIL with `package run-project/internal/config is not in std` or undefined config symbols.

- [ ] **Step 3: Implement schema, duration codec, and exact defaults**

Implement types above. `Duration.UnmarshalJSON` must reject numbers and invalid/negative strings. `Default` returns version 1, 5-second stop, 10 MiB, 5 files total, 10,000 lines, 5 attempts, 1-minute window, 1-second initial backoff, and 30-second maximum backoff:

```go
func Default() Config {
    return Config{
        Version: 1,
        Defaults: Defaults{
            StopTimeout: Duration(5 * time.Second),
            Log: LogConfig{MaxSizeMB: 10, MaxFiles: 5, BufferLines: 10000},
            Restart: RestartConfig{
                MaxAttempts: 5,
                Window: Duration(time.Minute),
                InitialBackoff: Duration(time.Second),
                MaxBackoff: Duration(30 * time.Second),
            },
        },
        Projects: []Project{},
    }
}
```

- [ ] **Step 4: Write failing validation and resolution tests**

Table-test duplicate names, missing dependency, dependency cycle, shell args, invalid health URL/address, sanitized log collision, relative directories, defaults merging, and environment precedence. Include this cycle case:

```go
func TestValidateRejectsDependencyCycle(t *testing.T) {
    root := t.TempDir()
    cfg := config.Default()
    cfg.Projects = []config.Project{{
        Name: "shop", Directory: root,
        Processes: []config.Process{
            {Name: "api", Command: "api", DependsOn: []string{"web"}},
            {Name: "web", Command: "web", DependsOn: []string{"api"}},
        },
    }}
    err := cfg.Validate()
    if err == nil || !strings.Contains(err.Error(), "shop.api.dependsOn") || !strings.Contains(err.Error(), "cycle") {
        t.Fatalf("error = %v", err)
    }
}
```

- [ ] **Step 5: Implement validation and resolution**

Use `filepath.Abs`, `filepath.Join`, `os.Stat`, `url.ParseRequestURI`, `net.SplitHostPort`, and DFS colors (`unvisited`, `visiting`, `done`). Errors include JSON-like paths such as `projects[0].processes[1].dependsOn`. `Resolve` merges zero-valued process overrides with defaults, defaults health to `process`, restart policy to `never`, resolves directories/env files, loads `.env`, and emits sorted `KEY=VALUE` entries after parent → file → explicit merge. `SafeName` keeps ASCII letters, digits, `.`, `_`, and `-`, replaces every other rune with `_`, and prefixes `.`/`..` with `_`; validation rejects safe project-name collisions globally and safe process-name collisions within each project.

```go
func mergeEnv(parent []string, file, explicit map[string]string) []string {
    values := make(map[string]string, len(parent)+len(file)+len(explicit))
    for _, item := range parent {
        key, value, ok := strings.Cut(item, "=")
        if ok {
            values[key] = value
        }
    }
    for key, value := range file { values[key] = value }
    for key, value := range explicit { values[key] = value }
    keys := make([]string, 0, len(values))
    for key := range values { keys = append(keys, key) }
    sort.Strings(keys)
    out := make([]string, 0, len(keys))
    for _, key := range keys { out = append(out, key+"="+values[key]) }
    return out
}
```

- [ ] **Step 6: Write failing `.env` parser tests**

Cover blank/comments, `export`, unquoted, single quote, escaped double quote, duplicate key last-wins, malformed line, unterminated quote, and literal `$HOME` without expansion:

```go
func TestParseEnvDoesNotExpand(t *testing.T) {
    got, err := config.ParseEnv(strings.NewReader("A=$HOME\nB=\"$A value\"\n"))
    if err != nil { t.Fatal(err) }
    want := map[string]string{"A": "$HOME", "B": "$A value"}
    if !reflect.DeepEqual(got, want) { t.Fatalf("got %#v", got) }
}
```

- [ ] **Step 7: Implement non-executing `.env` parser**

Read with `bufio.Scanner`, raise scanner buffer to 1 MiB, strip optional `export `, split first `=`, validate key against `^[A-Za-z_][A-Za-z0-9_]*$`, parse single quotes literally, parse double quotes with `strconv.Unquote`, trim unquoted whitespace, and never expand `$`, backticks, escapes in unquoted values, or commands.

- [ ] **Step 8: Write failing store tests**

Test missing file creates default, malformed/empty existing files remain byte-identical after load failure, unknown JSON fields fail, and failed validation never replaces existing file:

```go
func TestLoadMalformedDoesNotOverwrite(t *testing.T) {
    path := filepath.Join(t.TempDir(), "config.json")
    before := []byte("{broken")
    if err := os.WriteFile(path, before, 0o600); err != nil { t.Fatal(err) }
    if _, err := config.Load(path); err == nil { t.Fatal("expected error") }
    after, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if !bytes.Equal(after, before) { t.Fatalf("file changed: %q", after) }
}
```

- [ ] **Step 9: Implement paths and atomic store**

`Load` creates parent plus `Default()` only on `fs.ErrNotExist`; empty project list validates as bootstrap config. It opens existing file, rejects empty data and unknown fields with `json.Decoder.DisallowUnknownFields`, ensures one JSON value, validates, and returns config. `Save` validates first, marshals indented JSON with trailing newline, writes mode `0600`, syncs, closes, calls platform `replaceFile`, then syncs parent on Linux/macOS. Windows uses `windows.MoveFileEx` with `MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH`; Unix uses `os.Rename`. Task 1 runs `go mod tidy`; only dependencies used at this stage remain. Task 7 adds pinned Charm dependencies when TUI imports appear.

- [ ] **Step 10: Verify Task 1 and commit**

```bash
gofmt -w internal/config
go test ./internal/config -race
go mod tidy
git add go.mod go.sum internal/config
git commit -m "feat: add validated config store"
```

Expected: all config tests PASS; commit created.

---

### Task 2: Bounded Log Store and Rotation

**Files:**
- Create: `internal/logstore/logstore.go`
- Create: `internal/logstore/rotate.go`
- Test: `internal/logstore/logstore_test.go`
- Test: `internal/logstore/rotate_test.go`

**Interfaces:**
- Consumes: `config.LogConfig` from Task 1.
- Produces:

```go
type Stream string
const (
    Stdout Stream = "stdout"
    Stderr Stream = "stderr"
)

type Record struct {
    At     time.Time
    Stream Stream
    Text   string
}

type Event struct {
    Project string
    Process string
    Records []Record
    Err     error
}

type Filter struct {
    Stream Stream
    Query  string
}

type Store struct { /* private synchronized state */ }
type Handle struct { /* private stdout/stderr writers and sink */ }

func New(root string, batchEvery time.Duration) *Store
func (s *Store) Events() <-chan Event
func (s *Store) Open(project, process string, cfg config.LogConfig) (*Handle, error)
func (s *Store) Snapshot(project, process string) []Record
func (s *Store) Query(project, process string, filter Filter) []Record
func (s *Store) Close() error
func (h *Handle) Stdout() io.Writer
func (h *Handle) Stderr() io.Writer
func (h *Handle) Close() error
```

- [ ] **Step 1: Write failing ring and stream-tag tests**

```go
func TestStoreEvictsOldestAndTagsStreams(t *testing.T) {
    store := logstore.New(t.TempDir(), time.Millisecond)
    t.Cleanup(func() { _ = store.Close() })
    h, err := store.Open("shop", "api", config.LogConfig{MaxSizeMB: 1, MaxFiles: 2, BufferLines: 2})
    if err != nil { t.Fatal(err) }
    _, _ = io.WriteString(h.Stdout(), "one\ntwo\n")
    _, _ = io.WriteString(h.Stderr(), "three\n")
    if err := h.Close(); err != nil { t.Fatal(err) }
    got := store.Snapshot("shop", "api")
    if len(got) != 2 || got[0].Text != "two" || got[1].Stream != logstore.Stderr {
        t.Fatalf("records = %#v", got)
    }
}
```

- [ ] **Step 2: Run log tests and verify red state**

```bash
go test ./internal/logstore -run TestStoreEvictsOldestAndTagsStreams -v
```

Expected: FAIL because `internal/logstore` does not exist.

- [ ] **Step 3: Implement records, ring, and line writers**

Use one mutex-protected fixed-capacity ring per process. Stream writers preserve partial data between writes, split `\n`, remove one trailing `\r`, and emit chunks at 1 MiB when no newline arrives. `Handle.Close` flushes remaining partial records once. `Snapshot` returns copied records in chronological order.

```go
const maxRecordBytes = 1 << 20

func matches(record Record, filter Filter) bool {
    if filter.Stream != "" && record.Stream != filter.Stream { return false }
    return filter.Query == "" || strings.Contains(strings.ToLower(record.Text), strings.ToLower(filter.Query))
}
```

- [ ] **Step 4: Write failing query, chunking, and batch tests**

Prove case-insensitive filtering, stdout/stderr selection, a `maxRecordBytes+10` input becomes two records without data loss, and events contain short batches. Event test uses a 5-millisecond batch interval and a 1-second context timeout; it must not use sleeps.

- [ ] **Step 5: Implement non-blocking UI batching**

Each append updates ring and file sink before queuing UI notification. One goroutine coalesces pending records by process and flushes every `batchEvery`; `Events` has capacity 64. If event channel is full, merge records into pending batch rather than block child pipe readers. Warnings remain pending until delivered. `Store.Close` flushes batches, closes handles once, then closes event channel.

- [ ] **Step 6: Write failing rotation tests**

Use an internal byte limit constructor in tests to avoid writing MiB. Assert current plus four archives when `MaxFiles=5`, oldest deletion, record order across files, and sink reopen after directory permissions are restored where OS permits permission errors.

```go
func TestRotationKeepsConfiguredTotal(t *testing.T) {
    dir := t.TempDir()
    w, err := newRotatingWriter(filepath.Join(dir, "api.log"), 8, 3)
    if err != nil { t.Fatal(err) }
    for _, line := range []string{"1111\n", "2222\n", "3333\n", "4444\n"} {
        if _, err := w.Write([]byte(line)); err != nil { t.Fatal(err) }
    }
    if err := w.Close(); err != nil { t.Fatal(err) }
    matches, err := filepath.Glob(filepath.Join(dir, "api.log*"))
    if err != nil { t.Fatal(err) }
    if len(matches) != 3 { t.Fatalf("files = %v", matches) }
}
```

- [ ] **Step 7: Implement rotation and warning retry**

Rotate before a write that would exceed limit. Rename `file.3` to deletion, `file.2` to `file.3`, `file.1` to `file.2`, current to `file.1`; with `MaxFiles=1`, truncate by replacing current. Build paths only from `config.SafeName(project)` and `config.SafeName(process)`. File errors emit persistent warning events; next record retries opening sink while ring capture continues.

- [ ] **Step 8: Verify Task 2 and commit**

```bash
gofmt -w internal/logstore
go test ./internal/logstore -race
git add internal/logstore
git commit -m "feat: capture and rotate process logs"
```

Expected: all logstore tests PASS; race detector clean.

---

### Task 3: Health Checks and Restart Budget

**Files:**
- Create: `internal/process/health.go`
- Create: `internal/process/restart.go`
- Test: `internal/process/health_test.go`
- Test: `internal/process/restart_test.go`

**Interfaces:**
- Consumes: resolved `config.HealthConfig` and `config.RestartConfig`.
- Produces:

```go
type AliveFunc func() bool

func WaitHealthy(ctx context.Context, cfg config.HealthConfig, alive AliveFunc) error

type RestartTracker struct { /* private attempt timestamps */ }
func (t *RestartTracker) Next(cfg config.RestartConfig, policy string, expected bool, exitCode int, now time.Time) (time.Duration, bool)
func (t *RestartTracker) MarkHealthy(since, now time.Time, window time.Duration)
```

- [ ] **Step 1: Write failing health tests**

Use `httptest.NewServer` for HTTP success and terminal failure cases, `net.Listen("tcp", "127.0.0.1:0")` for TCP, injected alive functions for process checks, and context deadlines for timeout/cancellation. Assert HTTP accepts 200–399 and rejects 500 until timeout. Avoid sleep-based status transitions; each test starts endpoint in final state.

```go
func TestProcessHealthRequiresSurvivalInterval(t *testing.T) {
    cfg := config.HealthConfig{Type: "process", Interval: config.Duration(10 * time.Millisecond), Timeout: config.Duration(time.Second)}
    if err := process.WaitHealthy(context.Background(), cfg, func() bool { return true }); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Run health tests and verify red state**

```bash
go test ./internal/process -run 'TestProcessHealth|TestHTTPHealth|TestTCPHealth' -v
```

Expected: FAIL with undefined `process.WaitHealthy`.

- [ ] **Step 3: Implement cancellable health checks**

Wrap caller context with configured timeout. Process type waits one interval then checks alive. HTTP uses dedicated `http.Client`, GET requests bound to context, closes response body, and accepts 200–399. TCP uses `net.Dialer.DialContext`. HTTP/TCP retry on interval until success, caller cancellation, or timeout. Return errors containing health type and final probe error.

- [ ] **Step 4: Write failing restart-policy tests**

Use fixed timestamps; cover expected exits, exit zero, non-zero, signal represented as `-1`, exponential delays `1s,2s,4s`, 30-second cap, 5 attempts in 1 minute, rolling expiration, and healthy-window reset.

```go
func TestRestartBudget(t *testing.T) {
    cfg := config.RestartConfig{MaxAttempts: 2, Window: config.Duration(time.Minute), InitialBackoff: config.Duration(time.Second), MaxBackoff: config.Duration(30 * time.Second)}
    var tracker process.RestartTracker
    now := time.Unix(100, 0)
    if delay, ok := tracker.Next(cfg, "on-failure", false, 1, now); !ok || delay != time.Second { t.Fatalf("first = %s %v", delay, ok) }
    if delay, ok := tracker.Next(cfg, "on-failure", false, 1, now.Add(time.Second)); !ok || delay != 2*time.Second { t.Fatalf("second = %s %v", delay, ok) }
    if _, ok := tracker.Next(cfg, "on-failure", false, 1, now.Add(2*time.Second)); ok { t.Fatal("budget should be exhausted") }
}
```

- [ ] **Step 5: Implement restart tracker**

Expected exit always returns false. `never` returns false. `on-failure` returns false only for exit code zero. `always` restarts all unexpected exits. Prune timestamps older than rolling window before checking budget. Delay is `min(initialBackoff*2^attemptIndex, maxBackoff)`. `MarkHealthy` clears attempts only when `now.Sub(since) >= window`; manager schedules that check on a cancellable timer for each running generation.

- [ ] **Step 6: Verify Task 3 and commit**

```bash
gofmt -w internal/process
go test ./internal/process -race
git add internal/process/health.go internal/process/health_test.go internal/process/restart.go internal/process/restart_test.go
git commit -m "feat: add health and restart policies"
```

Expected: health and restart tests PASS.

---

### Task 4: Cross-Platform Process Trees

**Files:**
- Create: `internal/procgroup/group_unix.go`
- Create: `internal/procgroup/group_windows.go`
- Test: `internal/procgroup/group_unix_test.go`

**Interfaces:**
- Consumes: prepared `*exec.Cmd` whose stdout/stderr pipes are already attached.
- Produces same package API on all supported OS builds:

```go
type Group struct { /* OS-specific private fields */ }
func Start(cmd *exec.Cmd) (*Group, error)
func (g *Group) PID() int
func (g *Group) Graceful() error
func (g *Group) Force() error
func (g *Group) Wait() error
func (g *Group) Close() error
```

- [ ] **Step 1: Write failing Unix process-tree test**

Build-tag test `linux || darwin`. Re-exec current test binary in helper mode; helper starts `sh -c 'sleep 60 & echo $!; wait'`. Read descendant PID, call `Force`, `Wait`, then poll with deadline until `syscall.Kill(pid, 0)` returns `ESRCH`. Register `t.Cleanup` force kill before assertions to prevent leaked children.

- [ ] **Step 2: Run Unix test and verify red state**

```bash
go test ./internal/procgroup -run TestForceKillsDescendants -v
```

Expected: FAIL because package/API does not exist.

- [ ] **Step 3: Implement Unix group**

Use build tag `linux || darwin`. Before `cmd.Start`, set `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`. Store PID after start. `Graceful` calls `syscall.Kill(-pid, syscall.SIGTERM)`, `Force` uses `SIGKILL`, and both treat `ESRCH` as success. `Wait` calls `cmd.Wait` exactly once and returns captured result to all callers. `Close` force-stops un-waited process and reaps it.

- [ ] **Step 4: Implement Windows Job Object**

Use build tag `windows`. Create Job Object; set `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` through `JOBOBJECT_EXTENDED_LIMIT_INFORMATION`; add `CREATE_NEW_PROCESS_GROUP` to `SysProcAttr.CreationFlags`; start command; open process with `PROCESS_SET_QUOTA|PROCESS_TERMINATE`; assign it to job immediately; close process handle. Document unavoidable Go `os/exec` start-to-assignment race with `ponytail: Go os/exec does not expose suspended primary thread; replace with direct CreateProcess(CREATE_SUSPENDED) if escaped descendants are observed`. On setup failure terminate/reap child and close job. `Graceful` calls `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, uint32(pid))`; unsupported signaling returns error for caller to report while timeout still leads to `Force`. `Force` calls `TerminateJobObject`. `Close` closes Job Object after wait.

```go
info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
_, err := windows.SetInformationJobObject(
    job,
    windows.JobObjectExtendedLimitInformation,
    uintptr(unsafe.Pointer(&info)),
    uint32(unsafe.Sizeof(info)),
)
```

- [ ] **Step 5: Cross-compile OS adapter and run Unix test**

```bash
gofmt -w internal/procgroup
go test ./internal/procgroup -race
GOOS=linux GOARCH=amd64 go test -c ./internal/procgroup -o /tmp/procgroup-linux.test
GOOS=darwin GOARCH=amd64 go test -c ./internal/procgroup -o /tmp/procgroup-darwin.test
GOOS=windows GOARCH=amd64 go test -c ./internal/procgroup -o /tmp/procgroup-windows.test.exe
rm -f /tmp/procgroup-linux.test /tmp/procgroup-darwin.test /tmp/procgroup-windows.test.exe
```

Expected: local tests PASS; all three test binaries compile.

- [ ] **Step 6: Commit OS adapter**

```bash
git add internal/procgroup
git commit -m "feat: manage cross-platform process trees"
```

---

### Task 5: Process Lifecycle Manager

**Files:**
- Create: `internal/process/command.go`
- Create: `internal/process/manager.go`
- Test: `internal/process/manager_test.go`

**Interfaces:**
- Consumes: `config.ResolvedProcess`, `logstore.Store`, `procgroup.Group`, `WaitHealthy`, `RestartTracker`.
- Produces:

```go
type Key struct {
    Project string
    Process string
}

type State string
const (
    Stopped    State = "stopped"
    Starting   State = "starting"
    Running    State = "running"
    Stopping   State = "stopping"
    Restarting State = "restarting"
    Failed     State = "failed"
    Blocked    State = "blocked"
)

type Snapshot struct {
    Key          Key
    State        State
    PID          int
    StartedAt    time.Time
    ExitCode     int
    RestartCount int
    Error        string
}

type Event struct { Snapshot Snapshot }

type Manager struct { /* private entry map and event channel */ }
func NewManager(logs *logstore.Store) *Manager
func (m *Manager) Events() <-chan Event
func (m *Manager) Start(ctx context.Context, cfg config.ResolvedProcess) error
func (m *Manager) Stop(ctx context.Context, key Key) error
func (m *Manager) Restart(ctx context.Context, key Key) error
func (m *Manager) Block(ctx context.Context, key Key, reason string) error
func (m *Manager) Snapshot(key Key) (Snapshot, bool)
func (m *Manager) Snapshots() []Snapshot
func (m *Manager) Shutdown(ctx context.Context) error
func (m *Manager) ForceShutdown(ctx context.Context) error
```

- [ ] **Step 1: Write failing command-construction tests**

Assert direct mode preserves argument boundaries and shell mode maps to correct OS shell. Assert working directory and resolved environment are copied onto command. Keep helper function package-private and test in package `process`:

```go
func TestBuildCommandDirect(t *testing.T) {
    cfg := config.ResolvedProcess{Command: "tool", Args: []string{"a b", "c"}, Directory: t.TempDir(), Env: []string{"A=1"}}
    cmd := buildCommand(cfg)
    if cmd.Path != "tool" || !reflect.DeepEqual(cmd.Args, []string{"tool", "a b", "c"}) {
        t.Fatalf("command = %q", cmd.Args)
    }
}
```

- [ ] **Step 2: Implement direct and shell command construction**

Direct mode uses `exec.Command(command, args...)`. Shell mode uses `/bin/sh -c command` on Linux/macOS and `cmd.exe /C command` on Windows selected by `runtime.GOOS`. Set `Dir`, copy `Env`, leave stdin nil, and never use Bubble Tea `ExecProcess`. Do not bind child lifetime to action context: manager owns termination through process groups; caller context only bounds readiness/action waiting.

- [ ] **Step 3: Write failing lifecycle tests with helper modes**

Re-exec test binary using `-test.run=TestProcessHelper --` and environment mode values:

- `wait`: print ready line and wait for termination
- `exit-1`: exit code 1
- `stdout-stderr`: print one line to each stream then wait
- `ignore-term`: ignore graceful signal on Unix so forced timeout is exercised

Tests assert state sequence `starting → running → stopping → stopped`, log capture, PID, duplicate start no-op, start error `failed`, forced timeout, and no restart after user stop. Every test registers manager shutdown cleanup before starting.

- [ ] **Step 4: Run lifecycle tests and verify red state**

```bash
go test ./internal/process -run 'TestManagerStartStop|TestManagerCapturesLogs|TestManagerStartFailure' -v
```

Expected: FAIL with undefined `NewManager` or lifecycle methods.

- [ ] **Step 5: Implement serialized per-key state machine**

Each entry owns one mutex, resolved config, group, log handle, cancel function, restart tracker, expected-stop flag, and completion channel. `Start` creates entry if absent, rejects only conflicting `stopping`, emits `starting`, opens log handle, starts group, and launches `Wait` goroutine immediately. It then selects between process completion and `WaitHealthy`; only successful health emits `running`. State event send never occurs while holding entry mutex. `Snapshot(s)` copy all fields; sort by project then process.

Manager event delivery preserves every transition without blocking lifecycle workers: `emit` appends under a short queue mutex and non-blockingly notifies one delivery goroutine; delivery drains FIFO to public event channel. Shutdown stops producers, drains queue, then closes public channel.

`Stop` sets expected-stop before signaling. Call `Graceful`, wait for completion or stop timeout, then `Force`; always call `Wait` and close logs. Return `errors.Join` of meaningful signaling/wait errors, excluding normal exit status caused by requested termination. `Block` expected-stops active process and sets `blocked` with reason.

- [ ] **Step 6: Write failing automatic-restart tests**

Use 5-millisecond backoffs in resolved config. Assert `never`, `on-failure`, `always`, budget exhaustion, cancellation during backoff, and `Shutdown` suppresses restarts. Observe state through events with context deadlines, not sleeps.

- [ ] **Step 7: Implement restart loop and shutdown**

Unexpected wait result calculates exit code (`ExitError.ExitCode`, otherwise `-1`), consults tracker, emits `restarting`, waits on cancellable timer, and starts same resolved config. Increment restart count only for attempted restarts. When budget ends emit `failed`. `Restart` performs expected stop then fresh start with restart budget preserved. `Shutdown` atomically blocks new starts, cancels pending health/backoff work, stops all active entries concurrently, waits all, closes events, and joins errors. `ForceShutdown` cancels pending work, calls `Force` concurrently on every active group, waits/reaps each group within caller context, and never sends graceful signals.

- [ ] **Step 8: Verify Task 5 and commit**

```bash
gofmt -w internal/process
go test ./internal/process -race
git add internal/process/command.go internal/process/manager.go internal/process/manager_test.go
git commit -m "feat: add process lifecycle manager"
```

Expected: all process tests PASS; no race or leaked helper process.

---

### Task 6: Dependency and Project Controller

**Files:**
- Create: `internal/controller/graph.go`
- Create: `internal/controller/controller.go`
- Test: `internal/controller/controller_test.go`

**Interfaces:**
- Consumes: validated `config.Config` and concrete `process.Manager`.
- Produces:

```go
type ProcessSnapshot struct {
    Name      string
    DependsOn []string
    Runtime   process.Snapshot
}

type ProjectSnapshot struct {
    Name      string
    Directory string
    Processes []ProcessSnapshot
}

type Snapshot struct { Projects []ProjectSnapshot }

type Event struct { Snapshot Snapshot }

type Controller struct { /* config, graph, manager, event loop */ }
func New(cfg config.Config, manager *process.Manager) (*Controller, error)
func (c *Controller) Events() <-chan Event
func (c *Controller) Snapshot() Snapshot
func (c *Controller) Start(ctx context.Context) error
func (c *Controller) StartProcess(ctx context.Context, project, name string) error
func (c *Controller) StopProcess(ctx context.Context, project, name string) error
func (c *Controller) RestartProcess(ctx context.Context, project, name string) error
func (c *Controller) StartProject(ctx context.Context, project string) error
func (c *Controller) StopProject(ctx context.Context, project string) error
func (c *Controller) RestartProject(ctx context.Context, project string) error
func (c *Controller) ReplaceConfig(cfg config.Config) error
func (c *Controller) Shutdown(ctx context.Context) error
func (c *Controller) ForceShutdown(ctx context.Context) error
```

- [ ] **Step 1: Write failing graph tests**

Test topological levels for `db → api → web` plus independent `worker`, reverse levels, transitive dependencies, transitive dependents, and deterministic lexical order inside each level.

```go
func TestGraphLevels(t *testing.T) {
    g, err := newGraph([]config.Process{
        {Name: "db"},
        {Name: "api", DependsOn: []string{"db"}},
        {Name: "web", DependsOn: []string{"api"}},
        {Name: "worker"},
    })
    if err != nil { t.Fatal(err) }
    want := [][]string{{"db", "worker"}, {"api"}, {"web"}}
    if got := g.levels(); !reflect.DeepEqual(got, want) { t.Fatalf("levels = %#v", got) }
}
```

- [ ] **Step 2: Implement deterministic graph operations**

Use adjacency maps and indegree. `levels` returns Kahn layers sorted lexically. `reverseLevels` reverses layers. `dependencies(name)` and `dependents(name)` return transitive sets in topology order. Constructor rejects missing nodes/cycles even though config already validates, keeping controller boundary safe.

- [ ] **Step 3: Write failing controller integration tests**

Use real manager with helper processes whose process health becomes running after 5 milliseconds. Capture helper start timestamps via event channel to prove independent nodes start concurrently and dependent starts only after prerequisite running. Test:

- starting one process recursively starts dependencies
- stopping prerequisite stops transitive dependents first
- restarting prerequisite restores only dependents previously running
- project start/stop order
- project autostart selects all; otherwise process autostart only
- prerequisite crash blocks dependents and healthy restart restores them
- replacement config rejects changes to active process fields

- [ ] **Step 4: Run controller tests and verify red state**

```bash
go test ./internal/controller -run 'TestStartProcessOrdersDependencies|TestStopPrerequisiteStopsDependents|TestAutostart' -v
```

Expected: FAIL because controller package does not exist.

- [ ] **Step 5: Implement controller orchestration**

`Start` launches manager-event loop then computes autostart targets. For each topology layer use `errgroup` behavior implemented with `sync.WaitGroup`, buffered error channel, and `errors.Join`; do not add dependency. A process starts only after all prior layer starts return healthy. Stop uses reverse levels. Single-process stop includes transitive dependents. Record which dependents were running before interruption for restart restoration.

Manager event loop rebuilds immutable snapshot for every lifecycle transition. Controller uses same mutex-plus-notify FIFO delivery pattern as manager, preserving ordered snapshots without blocking startup before TUI begins consuming. On unexpected prerequisite transition to `failed`/`restarting`, stop/block running dependents. On return to `running`, restart recorded dependents in topology order. `ReplaceConfig` validates new config and refuses changed/removed active process definitions; stopped entries may change immediately. `ForceShutdown` delegates to manager, drains queued snapshots, and closes controller events once.

- [ ] **Step 6: Verify Task 6 and commit**

```bash
gofmt -w internal/controller
go test ./internal/controller -race
git add internal/controller
git commit -m "feat: orchestrate project dependencies"
```

Expected: all controller tests PASS; independent branch test proves concurrency without timing-only assertions.

---

### Task 7: Bubble Tea Dashboard and Safe Actions

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/dashboard.go`
- Create: `internal/tui/styles.go`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: controller snapshots/events, logstore events, config load/save callbacks.
- Produces:

```go
type Services struct {
    Snapshots      func() controller.Snapshot
    RuntimeEvents  <-chan controller.Event
    LogEvents      <-chan logstore.Event
    LogSnapshot    func(string, string) []logstore.Record
    LogQuery       func(string, string, logstore.Filter) []logstore.Record
    StartProcess   func(context.Context, string, string) error
    StopProcess    func(context.Context, string, string) error
    RestartProcess func(context.Context, string, string) error
    StartProject   func(context.Context, string) error
    StopProject    func(context.Context, string) error
    RestartProject func(context.Context, string) error
    Shutdown       func(context.Context) error
    ForceShutdown  func(context.Context) error
    Config         func() config.Config
    SaveConfig     func(config.Config) error
}

type Model struct { /* selected project/process, screen, dimensions, services */ }
func New(services Services) Model
func (m Model) Init() tea.Cmd
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() tea.View
```

Before first test, add exact TUI modules:

```bash
go get charm.land/bubbletea/v2@v2.0.8 charm.land/bubbles/v2@v2.1.1 charm.land/lipgloss/v2@v2.0.5
```

- [ ] **Step 1: Write failing navigation and resize tests**

Construct `Services{Snapshots: func() controller.Snapshot { return fixture }}`. Send `tea.WindowSizeMsg`, `tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})`, and right/left keys. Assert selected indices stay in bounds when project/process lists change. Assert `View().Content` contains text states `RUNNING`, `STOPPED`, selected marker `›`, and one-card layout below breakpoint.

- [ ] **Step 2: Run TUI tests and verify red state**

```bash
go test ./internal/tui -run 'TestDashboardNavigation|TestDashboardResize' -v
```

Expected: FAIL because TUI package does not exist.

- [ ] **Step 3: Implement base MVU model and accessible dashboard**

Use Bubble Tea v2 API: handle `tea.KeyPressMsg`, return `tea.View`, and set alternate screen on every view:

```go
func (m Model) View() tea.View {
    v := tea.NewView(m.render())
    v.AltScreen = true
    return v
}
```

`Init` waits independently for controller and log events with `tea.Batch`; each received event schedules next wait command. `WindowSizeMsg` updates dimensions. Render project cards with Lip Gloss v2 styles, text labels plus color, visible `›` focus marker, one column below 90 cells, two columns otherwise, and shortened footer below 70 cells. Empty config renders add-project guidance.

- [ ] **Step 4: Write failing action and confirmation tests**

Assert `s` schedules selected start, `g` opens project action menu, `k/r/q` open confirmations, cancel performs no callback, confirm invokes exactly one callback, active-process quit confirms, inactive quit calls shutdown directly, and operation errors remain visible.

```go
func TestStopRequiresConfirmation(t *testing.T) {
    calls := 0
    model := testModel(func(context.Context, string, string) error { calls++; return nil })
    updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: 'k'}))
    if cmd != nil { t.Fatal("stop ran before confirmation") }
    confirmed, cmd := updated.(tui.Model).Update(tea.KeyPressMsg(tea.Key{Code: 'y'}))
    if cmd == nil { t.Fatal("missing stop command") }
    _ = confirmed
    _ = cmd()
    if calls != 1 { t.Fatalf("calls = %d", calls) }
}
```

- [ ] **Step 5: Implement asynchronous actions and confirmations**

Never block `Update`. Each service call runs in `tea.Cmd` with bounded context; returns private `operationDoneMsg{err error}`. Confirmation stores exact pending action enum and target. `q` asks only when snapshots include `starting`, `running`, `stopping`, or `restarting`. On confirmed shutdown, render `Stopping processes…`, ignore new lifecycle keys, await callback, then return `tea.Quit` only after success or after displaying joined cleanup error and allowing forced exit.

- [ ] **Step 6: Verify Task 7 and commit**

```bash
gofmt -w internal/tui
go test ./internal/tui -race
git add internal/tui/model.go internal/tui/dashboard.go internal/tui/styles.go internal/tui/model_test.go
git commit -m "feat: add process dashboard TUI"
```

Expected: dashboard tests PASS and views contain textual state cues.

---

### Task 8: Interactive Log Viewer

**Files:**
- Create: `internal/tui/logview.go`
- Test: `internal/tui/logview_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**
- Consumes: `logstore.Snapshot`, `logstore.Query`, and Bubbles v2 viewport.
- Produces dashboard `enter` transition to log screen with follow, pause, search, and stream filtering.

- [ ] **Step 1: Write failing log-view tests**

Test `enter` opens selected process logs, `esc` returns, `f` toggles follow, manual up/page-up pauses follow, `t` cycles both → stdout → stderr, `/` opens search, Enter applies case-insensitive query, `n/N` moves matches, and new batches move to bottom only while following.

```go
func TestLogStreamCycle(t *testing.T) {
    m := logModelWithRecords([]logstore.Record{
        {At: time.Unix(1, 0), Stream: logstore.Stdout, Text: "out"},
        {At: time.Unix(2, 0), Stream: logstore.Stderr, Text: "err"},
    })
    updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 't'}))
    view := updated.(tui.Model).View().Content
    if !strings.Contains(view, "out") || strings.Contains(view, "err") { t.Fatalf("view = %q", view) }
}
```

- [ ] **Step 2: Run log-view tests and verify red state**

```bash
go test ./internal/tui -run 'TestLogStreamCycle|TestLogFollow|TestLogSearch' -v
```

Expected: FAIL with absent log-view state/behavior.

- [ ] **Step 3: Implement viewport and record formatting**

Create viewport with `viewport.New(viewport.WithWidth(width), viewport.WithHeight(height-3))`; update dimensions via setters. Format each record as local `15:04:05.000 OUT|ERR text`. Stream text remains visible without color. Build filtered lines from ring snapshot plus new batches. Set content with `SetContentLines`; call `GotoBottom` only when follow is true.

Search uses `textinput.New`, `SetWidth`, and Bubbles v2 updates. Compile literal case-insensitive matches with `regexp.QuoteMeta`, call viewport `SetHighlights`, `HighlightNext`, `HighlightPrevious`, and clear highlights when query empties. Search never mutates source records.

- [ ] **Step 4: Add high-volume update test**

Send 100 batches of 100 records directly as messages and assert model retains responsiveness, ring-derived visible content remains bounded by configured snapshot, and key message after batches toggles follow. This is deterministic state testing, not a performance threshold.

- [ ] **Step 5: Verify Task 8 and commit**

```bash
gofmt -w internal/tui
go test ./internal/tui -race
git add internal/tui/model.go internal/tui/logview.go internal/tui/logview_test.go
git commit -m "feat: add searchable live log viewer"
```

Expected: all log-view tests PASS.

---

### Task 9: Project and Process Forms

**Files:**
- Create: `internal/tui/form.go`
- Test: `internal/tui/form_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**
- Consumes: `config.Config`, `Config.Validate`, and `Services.SaveConfig`.
- Produces add/edit screens for project/process fields; saves whole config only after validation.

- [ ] **Step 1: Write failing project-form tests**

Assert `a` chooses add-project/add-process, project form edits name/directory/autostart, invalid directory keeps form open with exact field error, successful save invokes callback once, and `esc` discards changes. Test editing uses deep-copied config so cancellation cannot mutate live config.

- [ ] **Step 2: Write failing process-form tests**

Cover name, command, JSON args array, shell toggle, directory, env key/value editing, envFile, autostart, comma-separated dependencies, health enum and fields, restart enum/budget/backoff, log limits, and stop timeout. Assert shell mode rejects non-empty args. Assert rendered content includes env keys but never explicit values or loaded `envFile` contents.

```go
func TestProcessFormMasksEnvironmentValues(t *testing.T) {
    m := processFormModel(map[string]string{"TOKEN": "secret-value"})
    view := m.View().Content
    if !strings.Contains(view, "TOKEN") || strings.Contains(view, "secret-value") {
        t.Fatalf("unsafe view = %q", view)
    }
}
```

- [ ] **Step 3: Run form tests and verify red state**

```bash
go test ./internal/tui -run 'TestProjectForm|TestProcessForm' -v
```

Expected: FAIL with absent form behavior.

- [ ] **Step 4: Implement focused form fields**

Use Bubbles `textinput.Model` for scalar fields. Represent booleans and enums as keyboard-cycled fields; parse args using `json.Unmarshal` into `[]string`; parse dependencies by trim/split/dedupe; edit explicit env as key list plus password-echo value input. Existing values render masked and are unchanged unless replaced/deleted. Never read `envFile` into form.

Every save:

1. Deep-copy config through JSON marshal/unmarshal.
2. Apply typed form values to copy.
3. Call `Validate`.
4. If active process runtime-critical fields changed, open confirmation.
5. Invoke `SaveConfig` asynchronously.
6. Return dashboard only after callback succeeds; retain form and error on failure.

- [ ] **Step 5: Test active-process edit confirmation**

Assert changing only display-neutral values does not stop process; changing command, args, shell, directory, env/envFile, dependency, health, restart, or log settings requires confirmation. Confirm path calls stop, save, then optional restart in order; failed stop prevents save.

- [ ] **Step 6: Verify Task 9 and commit**

```bash
gofmt -w internal/tui
go test ./internal/tui -race
git add internal/tui/model.go internal/tui/form.go internal/tui/form_test.go
git commit -m "feat: edit runp config in TUI"
```

Expected: all form tests PASS; secret test proves no value disclosure.

---

### Task 10: Application Wiring, Signals, Documentation, and CI

**Files:**
- Modify: `main.go`
- Create: `internal/app/run.go`
- Create: `internal/app/signals_unix.go`
- Create: `internal/app/signals_windows.go`
- Test: `internal/app/run_test.go`
- Test: `internal/app/integration_test.go`
- Create: `README.md`
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: all packages from Tasks 1–9.
- Produces:

```go
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error
```

`main` calls `app.Run`, prints safe error to stderr, and exits non-zero on failure.

- [ ] **Step 1: Write failing flag and startup tests**

Inject temporary user paths by OS: Linux sets `XDG_CONFIG_HOME`/`XDG_CACHE_HOME`; macOS sets `HOME`; Windows sets `AppData`/`LocalAppData` environment keys used by `os.UserConfigDir`/`os.UserCacheDir`. Pass `--config`, assert missing file creation, malformed config error, unknown flag error, and configured path use. Keep `Run` testable without launching interactive TUI by an unexported `runProgram` function variable restored with `t.Cleanup`.

- [ ] **Step 2: Implement application wiring**

Parse args with `flag.NewFlagSet("runp", flag.ContinueOnError)`, only define `--config`, reject positional args, then:

1. Resolve/load config.
2. Resolve data dir and create log store.
3. Create process manager and controller.
4. Build `tui.Services` callbacks.
5. Start controller autostart.
6. Create Bubble Tea v2 program with `tea.NewProgram(tui.New(services), tea.WithInput(stdin), tea.WithOutput(stdout))`; alternate screen comes from `Model.View().AltScreen = true`.
7. Run until TUI shutdown completes.
8. Defer idempotent controller/log shutdown and join cleanup errors.

Wrap `Run` with recovery that converts panic to error containing stack trace after Bubble Tea restores terminal. Do not include environment values in errors.

- [ ] **Step 3: Write failing signal tests**

Abstract signal source as package-private channel parameter. First signal sends a typed shutdown request into Bubble Tea through `Program.Send`; second signal calls controller emergency cleanup then `Program.Kill`. Assert first waits for child exit and second path is bounded. Use channels, not sleeps.

- [ ] **Step 4: Implement signal lifecycle**

Production calls `notifySignals(ch chan<- os.Signal) func()` from build-tag files. Unix registers `os.Interrupt` and `syscall.SIGTERM`; Windows registers `os.Interrupt`. Returned function calls `signal.Stop(ch)`. First signal triggers normal TUI shutdown. Second signal invokes manager/process-group force cleanup, then `Program.Kill`. Keep both paths idempotent with separate `sync.Once` guards.

- [ ] **Step 5: Write full integration test**

Create temp config with `api` helper and dependent `worker`, temp log/cache roots, real controller/manager/log store, and no terminal renderer. Verify autostart dependency order, stdout/stderr records, restart after exit 1, manual project stop, restart project, then shutdown leaves helper PID nonexistent. Run through public package APIs; TUI rendering already has focused model tests.

- [ ] **Step 6: Run integration test and fix wiring only**

```bash
go test ./internal/app -run TestRunpWorkflow -v
```

Expected before wiring completion: FAIL on missing lifecycle behavior. Expected after minimal wiring: PASS with no remaining child.

- [ ] **Step 7: Write README**

Document installation, `runp --config`, default paths, complete version-1 JSON example, direct versus shell commands, dependency/health/restart rules, environment precedence and plaintext-secret warning, dashboard/log/form keys, graceful/forced stop behavior, and Linux/macOS/Windows support. State no daemon and no interactive stdin.

- [ ] **Step 8: Add CI matrix**

Create workflow triggered on push and pull request. Matrix `ubuntu-latest`, `macos-latest`, `windows-latest`; use `actions/checkout@v4`, `actions/setup-go@v5` with `go-version-file: go.mod`, run `go test ./...`, then `go build -o runp${{ runner.os == 'Windows' && '.exe' || '' }} .`. Add separate Ubuntu step `go test -race ./...`.

- [ ] **Step 9: Run full verification**

```bash
gofmt -w main.go internal
go mod tidy
go vet ./...
go test ./...
go test -race ./...
go build -o runp .
GOOS=linux GOARCH=amd64 go build -o /tmp/runp-linux .
GOOS=darwin GOARCH=amd64 go build -o /tmp/runp-darwin .
GOOS=windows GOARCH=amd64 go build -o /tmp/runp-windows.exe .
rm -f /tmp/runp-linux /tmp/runp-darwin /tmp/runp-windows.exe
```

Expected: vet clean; tests PASS; race detector clean; local and three target builds succeed.

- [ ] **Step 10: Manual smoke test**

Create temporary config for one harmless command that emits logs and waits. Launch `./runp --config <path>`, verify dashboard text and keyboard navigation, open logs, pause/follow/search/filter, restart, stop, edit config, and quit. After quit, confirm child PID no longer exists. Delete temporary config and binary.

- [ ] **Step 11: Commit final wiring**

```bash
git add main.go internal/app README.md .github/workflows/ci.yml go.mod go.sum
git commit -m "feat: ship runp process manager"
git status --short
```

Expected: commit created; working tree clean.

## Final Review Gate

Before integration choice, compare implementation against every acceptance criterion in `docs/superpowers/specs/2026-07-27-runp-design.md`. Required evidence:

```bash
go test ./...
go test -race ./...
go vet ./...
git status --short
git log --oneline --decorate -10
```

Confirm no helper child remains, no config/log fixture escaped temporary directories, no secret value appears in TUI test snapshots, and no unsupported OS code is compiled into Linux/macOS/Windows targets.
