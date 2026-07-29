# Semantic Constants Refactor Design

**Date:** 2026-07-28
**Status:** Approved

## Goal

Make production code easier to read by naming repeated or non-obvious literals. Preserve all behavior, public APIs, configuration schema, rendered text, keyboard controls, timeouts, file permissions, and platform behavior.

## Scope

Refactor production Go files across the repository. Change tests only when required to compile or preserve existing assertions. Do not rewrite test fixtures, documentation examples, or JSON samples.

Introduce constants only where a name communicates domain or operational meaning:

- Configuration version and default process settings.
- Health types and restart policies repeated across config, process, and TUI code.
- Shutdown deadlines, event queue capacities, log batching interval, record size, and scanner size limits.
- Config directory and file permissions.
- TUI default dimensions, responsive breakpoints, layout measurements, palette colors, and repeated form field identifiers.
- Special exit-code values whose meanings are not obvious.

Prefer standard-library constants where they already express intent, including HTTP status constants.

## Placement

Keep each constant in the production file or package that owns its meaning. Use existing domain constants across files only when doing so avoids duplicated concepts without creating package coupling. Do not add generic `constants.go` files.

Group related constants near their types or first use:

- Domain values near configuration or process types.
- Operational limits near constructors or loops that enforce them.
- TUI palette and layout constants in `internal/tui/styles.go`; form field identifiers near form definitions.

## Selection rule

Extract a literal when at least one condition holds:

1. It appears more than once and consistency matters.
2. Its numeric value does not explain its operational purpose.
3. It represents a domain vocabulary value prone to typo or drift.
4. Its name documents a boundary, capacity, timeout, permission, or layout rule.

Keep literals inline when local and self-explanatory, including zero-value checks, loop increments, one-off punctuation, and trivial slice indexes. Do not create aliases such as `zero = 0` or `one = 1`.

## Implementation constraints

- No new dependencies.
- No new abstractions, wrapper types, or files solely for constants.
- No changes to exported function signatures or serialized values.
- No behavior changes, including nil-versus-empty handling.
- Use shortest diff that gives each selected literal a clear name.
- Run formatter, full tests, and static diagnostics after changes.

## Verification

Existing tests remain primary regression coverage because refactor must be behavior-neutral. Add no new tests for constant aliases. Run:

1. `gofmt` on changed Go files.
2. `go test ./...`.
3. Workspace diagnostics for changed files.

## Deliberate ceiling

`ponytail:` constants improve literal clarity only; add stronger domain types such as `HealthType` or `RestartPolicy` only when invalid values cause real defects or API evolution requires compile-time enforcement.
