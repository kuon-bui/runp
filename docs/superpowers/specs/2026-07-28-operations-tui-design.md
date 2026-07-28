# Operations TUI Design

**Date:** 2026-07-28
**Status:** Approved

## Goal

Redesign all `runp` screens as a dense operations console that uses the full terminal, keeps projects, processes, PID, and selected-process logs visible, and presents forms as web-like modals. Preserve existing config, lifecycle, validation, logging, keyboard access, and secret masking.

## Scope

Included:

- Full-terminal dashboard with responsive project, process, and live-log panes.
- PID display from existing `process.Snapshot.PID`.
- Full-screen log viewer restyled to match the dashboard.
- Project and process forms rendered as modal overlays.
- Confirmation, project, and add menus rendered as small overlays.
- Text editing at the cursor with arrow keys.
- Form scrolling and field-level parse errors.

Excluded:

- CPU and memory collection.
- Config schema or lifecycle changes.
- Mouse interaction.
- New dependencies.

`ponytail:` resource metrics are excluded because portable CPU and memory sampling needs a separate design; add them when operators need trend or saturation data.

## Visual Language

Use a dark operations-console palette:

- Cyan accent for active panes and selection.
- Green for running and healthy states.
- Yellow for starting, stopping, and restarting states.
- Red for failed and blocked states.
- Muted gray for stopped states, borders, labels, and disabled content.

Use square, compact panes rather than padded cards. Pane bodies have one-cell left and right padding; root header and footer have none. Remove outer padding that leaves unused terminal space. Headers use uppercase labels. Selection uses both a marker and contrast change so color is not the only signal.

## Root Layout

Every screen renders within the latest Bubble Tea window size. The root renderer crops or pads content to exactly the available width and height, with minimum dimensions clamped to one cell. Header and footer each consume one row. Main content receives every remaining row.

### Wide terminals

At widths of 100 columns or more, dashboard main content has three fixed-purpose panes:

1. **Projects:** 20% of available width, clamped to 16–24 columns.
2. **Processes:** 38% of available width, clamped to 30–46 columns.
3. **Live log:** all remaining width.

Pane allocation must preserve usable minimum widths before applying percentages. The process pane shows `NAME`, `STATE`, and `PID`. The selected row uses `›`. PID is decimal while running and `—` when zero.

### Medium terminals

At widths from 70 through 99 columns, project and process navigation share the left region and live log occupies the right region. Project headings group process rows, preserving project context without a dedicated narrow pane.

### Narrow terminals

Below 70 columns, process rows occupy an upper region and live logs occupy the remaining lower region. Header shows selected project and process. Long names and log lines use viewport clipping rather than increasing rendered width.

### Height behavior

Pane bodies fill all rows between header and footer. Lists and logs use vertical viewports. Selection movement keeps the selected project and process visible. Empty space remains part of pane backgrounds, not unstyled terminal space.

## Dashboard Header and Footer

Header shows:

- `RUNP`.
- Selected project/process context, dropped before counts if complete header would exceed width.
- Project count.
- Running process count and total process count.

Footer shows only actions valid for current mode and terminal width. Compact terminals use shorter labels without changing keys. Existing dashboard keys remain:

- Arrow keys select project and process.
- `Enter` opens full log viewer.
- `s`, `k`, `r` start, stop, and restart selected process.
- `g` opens project actions.
- `a` opens add actions.
- `e` edits selected process.
- `q` and `Ctrl+C` request shutdown.

## Live Log Preview

Dashboard always previews logs for selected process. Selection changes immediately retarget preview. Runtime and matching log events refresh relevant panes. Preview follows newest output and displays only rows fitting current pane. Empty output displays `Waiting for output…`.

Preview uses existing `Services.LogSnapshot` and log event stream. Formatting is shared with full log viewer: local timestamp, `OUT` or `ERR`, then text. Preview does not add search, stream filtering, or independent scrolling; `Enter` opens existing full viewer for those operations.

Model owns preview viewport state. Rendering stays pure: service reads and viewport refreshes happen during initialization, selection changes, resize, and matching events, not inside `View`.

## Full Log Viewer

Full log viewer remains a separate full-screen mode. It adopts same compact header, pane border, colors, and footer as dashboard. Existing behavior remains:

- Arrow and page keys scroll.
- Upward scrolling disables follow.
- `f` toggles follow.
- `t` cycles both, stdout, and stderr.
- `/` edits search query.
- `n` and `N` move between matches.
- `Esc` returns to dashboard.

## Overlay Layers

Visual layers, highest priority first:

1. Confirmation overlay.
2. Project/add menu overlay.
3. Form modal.
4. Full log viewer.
5. Dashboard.

Only active top layer receives keyboard input. Dashboard state may still receive runtime and log events while covered.

Confirmation and action menus are compact centered boxes. Form modal is centered over a lower-contrast dashboard background. Compose overlays into rendered dashboard rows before returning `tea.View`; covered cells never bleed through modal content.

## Form Modal

Project and process forms open above dashboard rather than replacing it. Dashboard remains visible at lower contrast and cannot receive input.

Modal behavior:

- At 60 columns by 16 rows or larger, modal width is `min(90, terminalWidth-4)` and height is `terminalHeight-4`.
- Below either threshold, modal uses full terminal width and height.
- Header contains action and target, such as `Edit process · api`.
- Body scrolls vertically.
- Error summary sits immediately above footer.
- Footer shows `Ctrl+S Save` and `Esc Cancel`, plus section-specific hints.

Process form retains Basic, Environment, Health, Restart, and Logging sections. Wide modal uses section sidebar; narrow modal uses horizontal tabs. Project form omits section navigation.

## Web-Like Controls

Text fields render as label, bordered input box, optional field error, then spacing before next control. Focused input uses accent border and visible cursor. Unfocused input uses muted border. Long values scroll horizontally and keep cursor visible.

Control types:

- Text and numeric values: text input.
- Boolean values: switch-like toggle with visible on/off label.
- Health type and restart policy: select-like control with left/right indicators.
- Environment values: rows containing key, masked value, and delete affordance.

Environment values remain masked in every rendered state. Config storage behavior remains unchanged.

## Form Keyboard Behavior

- `Up` and `Down`: previous and next control.
- `Tab` and `Shift+Tab`: next and previous control.
- Text input `Left` and `Right`: move cursor without changing field.
- Text input `Home` and `End`: move to beginning and end.
- Text input `Backspace` and `Delete`: edit around cursor.
- Select `Left` and `Right`: cycle values.
- Toggle `Space`: switch value.
- Environment value `Enter`: set row using existing semantics.
- Environment key `Ctrl+X`: delete using existing semantics.
- `Ctrl+S`: validate and save.
- `Esc`: cancel without mutating live config.

Focus changes scroll body viewport enough to reveal full active control. Moving beyond first or last control wraps as current form behavior does.

## Validation and Errors

Parsing failures for JSON arguments, durations, and integers attach to corresponding fields. Field errors render directly below input. Errors not attributable to one field, including cross-field and full-config validation failures, render in modal error summary.

Saving invalid data keeps modal open and preserves entered values. Critical edits to active processes keep existing confirmation, stop, save, and optional restart flow. Failed stop still prevents save.

## Component Changes

### `internal/tui/dashboard.go`

- Accept width and height.
- Calculate responsive pane geometry.
- Render headers, project/process lists, PID, and preview pane.
- Keep state formatting independent from layout.

### `internal/tui/logview.go`

- Extract shared record-to-line formatting used by preview and full viewer.
- Keep full viewer filtering, search, follow, and scrolling behavior.

### `internal/tui/form.go`

- Separate text-input cursor keys from select cycling.
- Add modal-sized body viewport and focus visibility.
- Track parse errors by field while retaining summary error.
- Render web-like field, switch, select, and environment controls.

### `internal/tui/model.go`

- Own selected-process preview state and refresh it on selection, resize, and events.
- Compose dashboard and overlays.
- Route keys only to top active layer.
- Continue processing runtime and log events behind overlays.

### `internal/tui/styles.go`

- Define operations palette and semantic pane, modal, input, state, selection, and footer styles.
- Keep layout numbers as named constants.

No controller or process changes are required because PID already exists at `ProcessSnapshot.Runtime.PID`.

## Data Flow

1. Controller snapshot supplies projects, processes, runtime states, and PID.
2. Model clamps selection and derives selected project/process.
3. Model refreshes preview from log service for that selection.
4. Dashboard renderer receives snapshot, selection, preview viewport, and dimensions.
5. Runtime events replace snapshot and clamp selection.
6. Matching log events refresh preview and full viewer if open.
7. Window events resize dashboard panes, preview, full viewer, and modal viewport.

## Testing

Add focused tests for:

- Wide, medium, and narrow dashboard layouts.
- Final rendered width and height matching terminal dimensions.
- PID display and `—` for zero PID.
- Selection changes retargeting preview.
- Matching log events refreshing preview behind modal.
- Empty-log message.
- Centered modal and tiny-terminal full-screen fallback.
- Dashboard input blocked while modal or menu is open.
- Text `Left` and `Right` moving cursor and allowing mid-string edits.
- Select `Left` and `Right` cycling values.
- `Up`, `Down`, `Tab`, and `Shift+Tab` changing controls.
- Focus movement scrolling modal body.
- Field-level parse errors and summary validation errors.
- Environment secrets never appearing in rendered output.
- Existing save, cancel, critical edit, filtering, search, follow, and shutdown tests continuing to pass.

Prefer existing test fixtures and assert plain rendered content after stripping ANSI sequences. Add no snapshot-testing dependency.