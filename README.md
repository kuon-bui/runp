# runp

`runp` manages development processes from one foreground terminal UI. It starts, monitors, restarts, logs, and stops process trees for multiple projects. No daemon. Child stdin is not interactive.

## Install

Requires Go 1.26 or newer.

```sh
go install .
```

Run with default config:

```sh
runp
```

Use another config:

```sh
runp --config /path/to/config.json
```

Default paths:

- Config: `<os.UserConfigDir()>/runp/config.json`
- Logs: `<os.UserCacheDir()>/runp/logs/<project>/<process>.log`

Missing config gets created with version 1 and no projects. Malformed or invalid existing config returns error without overwrite.

## Configuration

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
            "policy": "on-failure",
            "maxAttempts": 5,
            "window": "1m",
            "initialBackoff": "1s",
            "maxBackoff": "30s"
          },
          "log": {
            "maxSizeMB": 10,
            "maxFiles": 5,
            "bufferLines": 10000
          }
        }
      ]
    }
  ]
}
```

### Commands

- `shell: false`: executes `command` and `args` directly. Portable default.
- `shell: true`: executes complete `command` through `/bin/sh -c` on Unix or `cmd.exe /C` on Windows. `args` must be empty. Quoting is platform-specific.

### Dependencies and health

`dependsOn` names processes in same project. Dependencies start first; dependents start only after dependencies report healthy. Stop order reverses dependency order.

Health types:

- `process`: ready after process survives one interval.
- `http`: ready after HTTP GET returns 200–399.
- `tcp`: ready after TCP connection succeeds.

Missing health config defaults to process health. Project `autostart: true` starts every process. Otherwise only process entries with `autostart: true` start.

### Restart

Policies: `never`, `on-failure`, `always`. Unexpected exits use exponential backoff and rolling attempt budget. User stop, project stop, and application shutdown never restart processes.

### Environment

Merge order, last value wins:

1. Parent `runp` environment
2. `envFile`
3. Explicit `env`

`.env` supports blank lines, `#` comments, optional `export `, and quoted or unquoted `KEY=VALUE`. No shell expansion or command substitution.

**Security:** JSON stores explicit environment values in plaintext. Form masks existing values, but config file and child logs can still contain secrets. Prefer external secret injection when possible.

## Keys

Dashboard fills terminal with responsive project, process, PID, and selected-process live-log panes. Forms open as keyboard-controlled modal overlays; small terminals use full-screen forms.

Dashboard:

- Arrow keys: select process/project
- `enter`: logs
- `s`: start process
- `k`: stop process, with confirmation
- `r`: restart process, with confirmation
- `g`: project start/stop/restart/edit menu
- `a`: add project/process
- `e`: edit selected process
- `q` or `Ctrl+C`: quit; active processes require confirmation

Log viewer:

- `Esc`: dashboard
- Arrow/Page keys: scroll; upward scrolling pauses follow
- `f`: follow/pause
- `t`: both/stdout/stderr filter
- `/`: search
- `n` / `N`: next/previous match

Forms:

- `Up` / `Down`, `Tab` / `Shift+Tab`: move between controls
- `Left` / `Right`: edit text at cursor or cycle selected values
- `Home` / `End`, `Backspace` / `Delete`: edit focused text
- `Space`: toggle focused boolean
- `Enter`: set explicit environment key/value
- `Ctrl+X`: delete explicit environment key
- `Ctrl+S`: validate and save
- `Esc`: cancel

## Shutdown and support

Normal stop sends graceful termination to whole process tree, waits configured timeout, then force-kills and reaps remaining children. First OS termination signal starts same cleanup. Second signal forces immediate tree cleanup and exits.

Supported: Linux, macOS, Windows.
