# Modern Process Form Design

**Date:** 2026-07-28
**Status:** Approved

## Goal

Replace flat process-field list with modern grouped editor while preserving config semantics, validation, keyboard access, secret masking, and save/restart behavior.

## Layout

Process form uses five groups:

1. Basic
2. Environment
3. Health
4. Restart
5. Logging

At widths of 80 columns or more, render group navigation as left sidebar and active group fields in right panel. Below 80 columns, render group navigation as horizontal tabs above panel. Only active group's fields appear.

Project form uses same visual shell without unnecessary group navigation because it has only two scalar fields and one boolean.

Header shows action and target, such as `New process` or `Edit process · api`. Optional validity/status text sits at header right when useful.

## Field presentation

Keep internal labels unchanged for config mapping. Add display metadata for friendly labels and group membership.

Examples:

- `HealthType` renders as `Health type`
- `RestartMaxAttempts` renders as `Max attempts`
- `LogBufferLines` renders as `Buffer lines`

Focused text input gets accent border/color. Unfocused inputs use muted border. Booleans render as chips, such as `✓ Autostart` and `○ Shell`. Enum values render as selectors, such as `‹ process ›`.

Environment keys render as sorted chips or compact masked entries. Values remain masked and must never appear in rendered output. Existing `EnvKey`, `EnvValue`, Enter-to-set, and Ctrl+X-to-delete semantics remain.

Validation errors render in dedicated error box immediately above footer. Footer shows context-relevant key hints.

## Keyboard behavior

- `Tab`, `Shift+Tab`, `Up`, `Down`: move between fields.
- Moving beyond active group's last or first field enters next or previous group.
- `Left`, `Right`: cycle enum values.
- `Space`: toggle focused boolean.
- `Enter`: set environment key/value when environment value is focused.
- `Ctrl+X`: delete selected environment key using existing key-input behavior.
- `Ctrl+S`: validate and save.
- `Esc`: cancel.

Sidebar and tabs are indicators, not separate focus modes. This avoids extra keyboard state and preserves fast linear navigation.

## Responsive state

`editForm` receives current terminal width and height from top-level model. Window resize updates active form before rendering. Width determines sidebar versus horizontal-tab mode. Rendering must degrade safely in narrow terminals without negative widths or panic.

No scrolling in first implementation. Grouping keeps each standard section within normal terminal height. If real usage shows clipped sections at small heights, add viewport scrolling later.

## Data and behavior boundaries

Do not change config schema, parsing, validation, controller persistence, critical-edit comparison, process lifecycle, or confirmation flow. Keep deep-copy behavior and nil-versus-empty preservation.

Use current Bubble Tea, Bubbles, and Lip Gloss dependencies. Add no dependency.

## Tests

Add focused tests for:

- Sidebar rendering at width 80 or greater.
- Horizontal tabs below width 80.
- Only active group fields rendered.
- Forward and backward boundary navigation changes group.
- Friendly labels and enum/boolean presentation.
- Resize propagation from `Model` to active form.
- Secret values never rendered.
- Existing save, toggle, enum, validation, and critical-edit behavior remains.

## Deliberate ceiling

`ponytail:` no vertical viewport initially; add one only when supported terminal heights prove grouped panels can still clip during normal use.
