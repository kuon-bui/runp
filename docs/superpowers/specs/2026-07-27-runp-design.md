# `runp` Process Manager TUI — Design

Date: 2026-07-27
Status: Approved

## 1. Goal

`runp` is a cross-platform terminal application for managing development-project processes. One `runp` session owns all child processes it starts. Users can start, stop, restart, inspect, and configure individual processes or whole projects through a Bubble Tea TUI. Exiting `runp` stops every owned process tree.

Supported operating systems:

- Linux
- macOS
- Windows

Out of scope:

- Keeping processes alive after `runp` exits
- Remote process management
- Attaching interactive stdin or a PTY to child processes
- Dependencies between different projects
- Resource monitoring such as CPU and memory charts
- Importing Docker Compose, Procfile, or package-manager configuration

## 2. Chosen Architecture

`runp` is one binary split into focused Go packages. It does not use a daemon.

### 2.1 Components

- **Bubble Tea TUI:** renders dashboard, log viewer, configuration forms, confirmations, and help. It sends typed actions and renders immutable snapshots/events. It never calls `os/exec` directly.
- **App Controller:** coordinates project-level actions, dependency order, autostart, process events, and application shutdown.
- **Process Manager:** owns child processes and implements start, graceful stop, forced stop, restart policies, health checks, and state transitions.
- **Config Store:** loads, validates, and atomically writes JSON configuration.
- **Log Store:** captures stdout/stderr, keeps bounded in-memory buffers, writes rotated files, and serves filtered snapshots to the TUI.
- **OS Adapter:** handles process-tree creation and termination through Unix process groups or Windows Job Objects.

Communication is event-driven. TUI actions enter the controller through method calls or typed channels. Runtime status and log batches return as Bubble Tea messages. Blocking work runs outside `Model.Update`; `Update` only changes model state and schedules commands.

### 2.2 Dependency policy

Required libraries:

- Bubble Tea for Model-View-Update TUI runtime
- Bubbles for viewport and input controls
- Lip Gloss for terminal layout and styling
- `golang.org/x/sys/windows` for Windows Job Object APIs unavailable in Go standard library

Go standard library handles JSON, HTTP/TCP checks, filesystem operations, command execution, synchronization, and Unix process control.

## 3. Configuration

### 3.1 Location and CLI

Default configuration path:

```text
<os.UserConfigDir()>/runp/config.json
```

Users may override it:

```text
runp --config <path>
```

No subcommands are required for first release. Running `runp` opens TUI.
If config file does not exist, `runp` creates parent directories and an empty version-1 config without project validation, then opens dashboard. An existing empty, malformed, or unreadable file is an error and is never overwritten automatically.

Runtime data path:

```text
<os.UserCacheDir()>/runp/
```

Logs live below:

```text
<runtime-data-path>/logs/<sanitized-project-name>/<sanitized-process-name>.log
```

### 3.2 JSON shape

```json
{
  "version": 1,
  "defaults": {
    "stopTimeout": "5s",
    "log": {
      "maxSizeMB": 10,
      "maxFiles": 5,
      "bufferLines": 10000
    },
    "restart": {
      "maxAttempts": 5,
      "window": "1m",
      "initialBackoff": "1s",
      "maxBackoff": "30s"
    }
  },
  "projects": [
    {
      "name": "shop",
      "directory": "/home/user/code/shop",
      "autostart": false,
      "processes": [
        {
          "name": "api",
          "command": "npm",
          "args": ["run", "dev"],
          "shell": false,
          "directory": "backend",
          "env": {
            "APP_ENV": "development"
          },
          "envFile": ".env",
          "autostart": true,
          "stopTimeout": "5s",
          "dependsOn": [],
          "health": {
            "type": "http",
            "url": "http://127.0.0.1:3000/health",
            "timeout": "30s",
            "interval": "500ms"
          },
          "restart": {
            "policy": "on-failure"
          }
        }
      ]
    }
  ]
}
```

Process `directory` is optional. Relative values resolve from project `directory`. `envFile` follows the same rule. Process `stopTimeout` is optional and overrides global default. Missing restart policy defaults to `never`.

Command modes:

- `shell: false`: execute `command` with `args` directly. This is default and portable mode.
- `shell: true`: treat `command` as complete shell source and require empty `args`. Unix executes `/bin/sh -c <command>`; Windows executes `cmd.exe /C <command>`. Configuration UI warns that syntax and quoting are platform-specific.

Environment merge order, last value winning:

1. Parent `runp` environment
2. `envFile`
3. Explicit `env`

`.env` parser accepts blank lines, comments beginning with `#`, optional `export ` prefix, and `KEY=VALUE` entries with single-quoted, double-quoted, or unquoted values. It does not perform shell expansion or command substitution. This prevents configuration loading from executing code.

### 3.3 Validation

Configuration must pass validation before any process starts or before edited configuration with projects replaces current configuration. Empty project list is valid bootstrap configuration even though no project directory exists.

- `version` equals `1`
- Project names are non-empty and unique
- Process names are non-empty and unique within project
- Project directory exists and is a directory
- Resolved process directory exists and stays unrestricted; users explicitly control commands they run
- Direct command has non-empty `command`
- Shell command has empty `args`
- Dependencies reference processes in same project
- Dependency graph is acyclic
- Durations and numeric limits are positive
- Restart policy is `never`, `on-failure`, or `always`
- Health type is `process`, `http`, or `tcp`
- HTTP checks require `http://` or `https://` URL
- TCP checks require valid `host:port`
- Sanitized log names cannot collide within project

Form edits are validated in memory first. Config Store writes JSON to a temporary file in same directory, calls `Sync`, renames it over target, then syncs parent directory where supported. Failed writes leave prior file intact.

## 4. Runtime Model

### 4.1 Process states

Each process has exactly one state:

- `stopped`: not running and not scheduled
- `starting`: child started; readiness not confirmed
- `running`: configured health check passed
- `stopping`: graceful or forced stop underway
- `restarting`: waiting for restart backoff
- `failed`: start failed or restart budget exhausted
- `blocked`: dependency failed, stopped, or missed readiness timeout

Snapshots expose PID when running, start time, exit code when known, restart count, latest error, and health state.

### 4.2 Start

Starting one process recursively starts its dependencies. Independent dependency branches start concurrently. A dependent starts only after every direct dependency reaches `running`.

Starting a project starts all project processes concurrently subject to dependency order. If project `autostart` is true, every process is selected at application startup. Otherwise, only processes with `autostart: true` are selected. Duplicate requests for a running or starting process are no-ops.

Readiness behavior:

- `process`: process is ready after it survives one health interval; default interval is `500ms`
- `http`: repeated GET requests; any `200-399` response is healthy
- `tcp`: repeated TCP dial attempts until connection succeeds

Missing health configuration defaults to `process`. Default health timeout is `30s`; default interval is `500ms`. A timeout stops process and marks it `failed`. Dependents become `blocked` with dependency name in error.

### 4.3 Stop and restart

Stopping one process also stops its transitive dependents first, in reverse dependency order. This avoids leaving dependents running against unavailable prerequisites.

Stopping a project stops all processes in reverse dependency order; independent branches stop concurrently.

Graceful stop:

1. Mark process `stopping`; disable automatic restart for this stop operation.
2. Request graceful termination for whole process tree.
3. Wait configured timeout, default `5s`.
4. Force-terminate remaining tree.
5. Wait for process reaping and close log streams.

Unix creates one process group per managed process. Graceful stop sends `SIGTERM` to group; forced stop sends `SIGKILL`.

Windows assigns each managed process tree to a Job Object. Graceful termination first sends best available console-control termination when process supports it. At timeout, closing or terminating Job Object kills remaining descendants. If graceful signaling is unavailable, state remains `stopping` until timeout, then Job Object termination runs.

Restart means stop to completion, reset readiness, then start. Restarting a prerequisite first stops transitive dependents, restarts prerequisite, waits for readiness, then starts previously running dependents in dependency order.

When a running prerequisite exits unexpectedly, controller stops its transitive dependents and marks them `blocked`. If prerequisite becomes healthy through automatic or manual restart, controller restarts only dependents that were running before interruption.

### 4.4 Automatic restart

Policies:

- `never`: never restart after exit
- `on-failure`: restart only when exit code is non-zero or process ends by unexpected signal
- `always`: restart after every unexpected exit

User stop, project stop, and application shutdown are expected exits and never trigger restart.

Restart attempts use exponential backoff from `1s`, doubling to maximum `30s`. Default budget is 5 attempts in rolling 1-minute window. Exceeding budget marks process `failed`. A process remaining healthy for full window clears attempt history.

### 4.5 Application shutdown

Quit requests show confirmation when any process is active. Confirmed quit blocks new start/restart actions, stops every process using normal project shutdown rules, aggregates errors, and exits TUI only after all children are reaped or forcibly terminated. Failure to stop one process does not skip others.

OS termination signals sent to `runp` initiate same shutdown path. A second termination signal forces immediate process-tree cleanup and exit.

## 5. Logging

Each process has one bounded ring buffer and one rotated file sink.

Each log record contains:

- Timestamp captured when line is read
- Stream: `stdout` or `stderr`
- Text without trailing newline

Stdout and stderr are read concurrently. Lines larger than scanner defaults are supported up to 1 MiB. Data exceeding 1 MiB before newline is emitted as bounded chunks rather than terminating log capture.

Default ring capacity is 10,000 records per process. Oldest records are discarded when full. UI receives log records in short batches to prevent one noisy process from starving input/rendering. File writes continue independently if UI cannot keep up.

Rotation defaults:

- Rotate when current file would exceed 10 MiB
- Keep 5 files total: current file plus up to 4 archived files
- Rename archives numerically, deleting oldest before rotation

Rotation size and archive count are configurable globally and overridable per process. A file-write failure raises persistent warning event but does not terminate process. `runp` periodically retries opening sink.

Log viewer supports:

- Live follow and pause
- Manual scrolling
- Case-insensitive substring search
- Stream filter: both, stdout, stderr
- Visible timestamps
- Reading current in-memory session only; archived-file browsing is out of scope

`envFile` contents are never loaded into form fields or diagnostic messages. Explicit `env` keys are shown, but values are masked; users can replace or delete values without revealing them. JSON remains plaintext configuration, so form warns against storing secrets there. Child output is not redacted because arbitrary output cannot be reliably classified as secret; form warns users that child processes control their own logs.

## 6. TUI Design

Bubble Tea runs in alternate-screen mode.

### 6.1 Dashboard

Dashboard uses project cards. Each card shows project name, path, aggregate status, process names, and process states. Selected project and process have distinct focus styles.

Keys:

- `up` / `down`: select project
- `left` / `right`: select process in project
- `enter`: open selected process log viewer
- `s`: start selected process
- `k`: stop selected process after confirmation
- `r`: restart selected process after confirmation
- `g`: open project action menu for start, stop, or restart all
- `a`: add project or process
- `e`: edit selected project or process
- `?`: open full help
- `q`: quit; confirm if processes are active

Terminal widths below layout breakpoint render one project card per row and shorten footer. Full key help remains available through `?`.

### 6.2 Log viewer

Log viewer header shows project/process, state, follow mode, and stream filter. Keys support scroll, follow toggle, stream cycling, search entry, next/previous match, and return to dashboard. New logs do not move viewport while follow is paused.

### 6.3 Forms

Forms edit project and process fields with Bubbles inputs/selectors. Save performs full configuration validation. Invalid fields retain focus and show specific messages. Successful save atomically persists entire config and updates controller configuration only when affected processes are stopped. Editing runtime-critical fields of active process requires confirmation, then stops process, saves config, and optionally restarts it.

### 6.4 Accessibility and safety

- State is communicated with text labels, not color alone
- Focus is visible without relying only on color
- All actions are keyboard accessible
- Destructive or disruptive actions require confirmation
- Errors remain visible until dismissed or resolved
- Styles degrade safely when terminal has limited color support

## 7. Error Handling

- Invalid config prevents process startup and opens error screen listing exact JSON path and reason.
- Start failure sets `failed`, stores safe error text, and leaves manual restart available.
- Health timeout stops failed process and blocks dependents.
- Missing dependency readiness reports dependency name and last health error.
- Log sink failure warns but keeps child running and in-memory capture active.
- Config write failure preserves old config and current runtime state.
- Shutdown attempts every remaining process and reports aggregated failures after cleanup.
- Panic recovery at application boundary restores terminal before printing diagnostic and returning non-zero exit status.

## 8. Concurrency and Data Ownership

Process Manager owns mutable runtime process state. Controller serializes lifecycle operations per project and allows independent projects to operate concurrently. Log Store owns ring buffers and file sinks.

TUI receives copied snapshots and batched events; it does not share mutable maps or buffers with runtime workers. Every long-running operation accepts context cancellation. Shutdown cancels health checks, pending backoff timers, and queued starts before stopping active children.

Backpressure rules:

- Lifecycle/state events are never intentionally dropped.
- Log UI batches may be coalesced when queue is full because complete current-session history remains in ring buffer and file.
- File logging has a bounded write queue per process; when full, reader writes synchronously to preserve logs rather than dropping data.

## 9. Testing

Unit tests cover:

- Config defaults, validation, `.env` parsing, atomic writes, missing dependencies, and cycles
- Dependency start/stop order, concurrent branches, group actions, and autostart
- Process state transitions, restart policies, rolling budget, and cancellation
- HTTP, TCP, and process health checks
- Ring buffer, stream tagging, large lines, filtering, searching, and rotation
- Bubble Tea updates for navigation, resize, actions, confirmations, and log-view modes

Integration tests use test-helper modes in Go test binary to emit stdout/stderr, wait, crash with chosen exit code, and spawn a child. Tests verify graceful shutdown and process-tree cleanup without requiring Node.js or Python.

Platform-specific tests verify Unix process groups and Windows Job Objects. CI runs `go test ./...`, race tests where supported, and builds for Linux, macOS, and Windows.

## 10. Acceptance Criteria

1. `runp` loads default config or path from `--config` and reports actionable validation errors.
2. Dashboard manages multiple projects with multiple processes each.
3. Users can start, stop, and restart one process or whole project.
4. Dependency order, process/HTTP/TCP readiness, and blocked state work as specified.
5. `never`, `on-failure`, and `always` restart policies obey backoff and restart budget.
6. Autostart follows project/process rules.
7. Live logs support scrolling, pause/follow, search, stream filtering, timestamps, bounded memory, and file rotation.
8. JSON and TUI forms both configure same model without corrupting existing config.
9. Confirmed quit and OS signals stop all owned process trees; no child remains after normal shutdown.
10. UI remains responsive under noisy logs and narrow terminal sizes.
11. State labels and keyboard navigation remain usable without color.
12. Tests pass and binary builds on Linux, macOS, and Windows.

## 11. Deliberate Simplifications

- No daemon: add only if processes must outlive TUI.
- No PTY/stdin attach: add only when interactive child applications become required.
- No archived-log browser: users can open rotated files with standard tools; add viewer only if operational use proves need.
- No cross-project dependencies: add only after concrete workflows require them.
- No plugin system or generic backend interfaces beyond OS boundary: add only when second real implementation exists.
