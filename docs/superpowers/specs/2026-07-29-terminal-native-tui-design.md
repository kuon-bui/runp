# Terminal-Native TUI Design

**Date:** 2026-07-29
**Status:** Approved

## Goal

Replace the heavy gray operations-console treatment with a compact terminal-native visual system across the dashboard, log viewer, forms, and overlays. Preserve all behavior, keyboard controls, configuration, process lifecycle, responsive breakpoints, validation, and secret masking.

## Root cause

The current form modal applies `colorSurfaceHigh` to nearly its entire responsive area. Empty modal cells therefore become a large gray slab over another filled background. Uniform square borders, equal-width inputs, low contrast between structural levels, and excessive unused modal area make the screen visually heavy and flatten hierarchy.

## Visual language

Use one dark terminal background across all screens. Do not add a second full-area surface behind dashboard or modal whitespace.

Use color semantically:

- Cyan marks current selection, focus, and active navigation only.
- Green marks running, healthy, or enabled state.
- Yellow marks transitional state and destructive confirmation titles.
- Red marks failed state and validation or operation errors.
- Muted gray marks labels, inactive navigation, stopped state, separators, and borders.

Unfocused controls use a subtle underline or muted boundary. Only focused and invalid controls gain stronger borders or backgrounds. Selection retains a visible marker so meaning never depends on color alone.

Group related form fields under small section labels. Keep one blank row between groups where available. Avoid rounded web cards, wide colored slabs, gradients, shadows, and decoration without operational meaning.

## Dashboard

Keep current responsive modes and information:

- Wide: projects, processes, and live log panes.
- Medium: grouped processes and live log panes.
- Narrow: process pane above live log pane.

Reduce chrome without changing pane geometry. Header and footer use the root dark background rather than a contrasting filled strip. Pane boundaries and titles use muted separators. Selected project or process uses cyan marker and restrained emphasis; it must not fill a wide bright row. Process state colors, PID, live preview, clipping, and viewport behavior stay unchanged.

## Log viewer

Use the same root background, muted separators, active cyan, and status colors as the dashboard. Preserve stream filtering, search, highlighting, follow mode, scrolling, and footer keys. Search focus may use the focused-control treatment; inactive metadata stays muted.

## Forms

Keep current responsive modal sizing, section sidebar on wide terminals, tabs on narrower terminals, viewport scrolling, cursor editing, control order, and save/cancel behavior.

Remove the modal-wide `colorSurfaceHigh` background. Modal whitespace uses the root terminal background. Keep only a subtle modal boundary when the terminal is large enough; tiny-terminal forms remain full screen.

Render controls with clear hierarchy:

- Section labels: small, muted, and visually separated from values.
- Text and numeric fields: label plus restrained value line; focused field gains cyan boundary or low-intensity cyan background.
- Select fields: same control treatment plus existing left/right semantics.
- Boolean fields: compact state text; green only when enabled.
- Environment values: continue masking secrets in every state.
- Field errors: red directly below their field.
- Summary errors: compact red line or box above footer, never a large filled panel.

Prefer content-shaped structure over empty framed area. If modal height exceeds visible content, remaining rows retain the root background rather than a raised surface.

## Overlays

Confirmation, project action, add action, busy, and operation-error overlays remain centered and keyboard-controlled. Render them as small dark boxes with a subtle muted or semantic border. Do not use a large gray background. Preserve current overlay priority and input routing.

## Data and behavior

No service, controller, config, process, or log data-flow changes. Existing `Model.View` layering remains:

1. Dashboard or log viewer.
2. Form overlay when active.
3. Menus and confirmation.
4. Busy or operation error.

Runtime and log events continue updating covered screens. No new dependency is needed; existing Lip Gloss styles and layout functions cover the redesign.

## Accessibility

Keep markers, labels, and text for every status and action so color is supplementary. Maintain readable foreground/background contrast on dark terminals. Focus must remain visible without relying only on hue. Keep all controls keyboard accessible and all existing key hints available at responsive widths.

## Testing

Retain all existing tests for responsive layout, exact terminal dimensions, selection, logs, overlays, forms, validation, lifecycle, and secret masking.

Add focused render tests that verify:

- Form modal no longer emits the raised-surface background across its empty area.
- Focused control styling differs from an unfocused control while both labels remain visible.
- Dashboard, log viewer, form, and compact overlays use the shared terminal-native palette.
- Existing responsive and exact-size guarantees still hold.

Use ANSI-aware assertions already present in the package. Run `gofmt` on changed Go files and `go test ./...`.

## Deliberate ceiling

`ponytail:` this redesign changes presentation only. Add user-selectable themes only when users need multiple terminal palettes; until then one accessible dark palette avoids configuration and compatibility cost.
