# Named Constants Refactor Design

**Date:** 2026-07-28
**Status:** Approved

## Goal

Make production code easier to read by naming repeated or non-obvious literals. Preserve behavior, config schema, rendered output, keyboard controls, timeouts, permissions, and public package APIs.

## Scope

Refactor production Go files across repository. Update tests only when compilation or existing assertions require it. Do not refactor test fixtures, README examples, or design documents.

Name literals when they represent domain vocabulary, defaults, limits, protocol boundaries, permissions, terminal dimensions, layout measurements, colors, or identifiers repeated across related logic. Keep literals local when their meaning is already obvious, such as zero-value checks, single-step increments, slice indexes, and one-off presentation text.

## Placement

Place each constant in file or package that owns its meaning. Reuse exported constants only when another package already depends on that concept. Do not add generic `constants.go` files or a repository-wide constants package.

Prefer standard-library constants where they express intent exactly, including HTTP status constants. Keep typed constants where compiler checking improves domain clarity.

## Package Changes

### Config

Name current config version, health types, restart policies, default durations and limits, valid TCP port bounds, and config directory/file permissions. Reuse health and restart constants within validation, resolution, and process behavior without changing JSON string values.

### App, controller, process, and log store

Name shutdown timeouts, log batching interval, signal/event channel capacities, record limits, and process exit sentinel values where meaning is non-obvious. Keep concurrency behavior and buffering unchanged.

### TUI

Name default terminal dimensions, responsive breakpoints, layout widths and spacing, Lip Gloss colors, repeated form field identifiers, enum values, and repeated labels used as internal keys. Keep exact rendering and input behavior unchanged.

## Readability Rules

- Use names describing purpose, not encoded value, such as `wideFormBreakpoint` rather than `width80`.
- Avoid aliases for `0`, `1`, empty string, or local punctuation unless they carry domain meaning.
- Avoid helper abstractions, registries, or enum wrappers introduced only to hold constants.
- Delete duplicate literals when one existing domain constant can serve all callers.
- Keep declarations near first use unless package-wide reuse makes a shared declaration clearer.

## Verification

Run `gofmt` on changed Go files, then run full Go test suite. Existing tests provide behavior lock because refactor must not change outputs or semantics. Add no new test unless refactor exposes non-trivial logic; constant substitution alone needs no new test.

## Deliberate Ceiling

`ponytail:` keep string-backed config fields and current public APIs; add dedicated domain types only when compile-time validation is needed across package boundaries.
