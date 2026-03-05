# AGENTS

## Purpose

This repo contains the Go-based Unikraft CLI. Use this file for contributor-facing guidance.

## Development practices

- Use `task` for builds and workflows; enable Remote Taskfiles with `TASK_X_REMOTE_TASKFILES=1`.
- Build the CLI with `task cli`; the binary is placed in `dist/`, and called `unikraft`.
- Build documentation with `task docs`; outputs include Markdown docs in `dist/docs/` and man pages in `dist/man/`.
- Quality gates: `task lint`, `task test`. Run these after making changes to ensure code quality.
- Integration tests: `task integration`. These tests cover end-to-end scenarios, don't run unless the user prompts you. You can update the expected outputs by running `task integration-update`.
- Run these locally before pushing; CI also runs them.

## Architecture overview

- Single CLI binary with a command tree that routes to internal services.
- Core `Resource` interface models API objects with `Type`, `Key`, `Fields`, and `Raw`, and is extended by get/list/create/edit/delete behaviors.
- Resource data is expressed as a tree of `Field` nodes: each node has a name, optional value, nested subfields, and an `Elem` template for homogeneous arrays; patch metadata and verbosity drive editing and output.
- Resource utilities live alongside the resource layer (inside `internal/`): struct↔field conversion, field cloning/merging/deduping, map/slice encoding, path-based filtering/selection, and patch helpers for edits.
- Core logic lives under `internal/` and is organized by domain: configuration/profile handling, API client communication, resource abstractions, output/logging utilities, multi-region support, and shared helpers.
- Build artifacts and generated docs go to `dist/`; developer tooling/scripts live at the repo root and under `tools/`.

## Important dependencies

- `kong` for CLI parsing and command/flag wiring
- `unikraft.com/cloud/sdk` for Unikraft Cloud API clients and types.
- `unikraft.com/x/...` repos for Unikraft-specific helpers (logging, colors, pointer utilities, terminal sizing, etc.).

## Advice

- If modifying existing functionality that has tests, add new tests to cover the changes in behavior.
- If wanting to explore Go documentation or inspect Go code, use the `go doc` command - don't explore the filesystem outside of the current directory for this.
- Use the newest Go features you know about; do not worry about old Go versions. For example:
  - Use `errors.Join` for combining errors.
  - Use the `slices`/`maps`/`cmp`/`iter` packages for common operations on slices/maps/comparisons/iteration.
  - Use `for i := range <n>` for iterating a fixed number of times.
  - No need to use `i := i` in loops to capture loop variables for closures; Go 1.21+ captures them by default.
  - Prefer `new(value)` to get pointers to literals now that Go 1.26 allows expressions in `new`.
