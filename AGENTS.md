# AGENTS.md

Guidance for AI agents (Claude Code, etc.) working in this repository.

## What this is

`go-pst` is a Go library for **reading** Personal Storage Table (`.pst`) and
Offline Storage Table (`.ost`) files — the on-disk format behind Microsoft
Outlook mail, appointments and contacts (the PFF/OFF family).

This checkout is the **`Private-Recall/go-pst` fork**, maintained to harden the
parser for real-world archives consumed by
[`Private-Recall/recall`](https://github.com/Private-Recall/recall). Upstream
`mooijtech/go-pst` is dormant — **land changes here**, not upstream.

- Module path is still `github.com/mooijtech/go-pst/v6` (unchanged so the fork
  drops in via a `replace` directive in recall's `go.mod`).
- Go 1.20.

## Build & test

```bash
go build ./...
go test ./pkg/          # the test suite lives in pkg/
go vet ./pkg/
```

- Tests load **real PST fixtures** from `data/`:
  - `32-bit.pst` — ANSI (Outlook 97–2002, `PT_STRING8`).
  - `enron.pst`, `support.pst` — Unicode. `support.pst` stores HTML bodies as
    `PidTagHtml` / `PtypBinary` (the modern-Outlook case).
  - Tests open these with `os.Open("../data/<name>.pst")` and `Fatal` if missing.
- `pkg/io_uring.go` is `//go:build linux` only. On Windows/macOS the library
  falls back to `DefaultReader`; `NewAsync`/io_uring paths are unavailable there.
  Don't write tests that assume async I/O off Linux.

### Fuzz the attacker-byte boundaries

This library consumes **untrusted, attacker-influenceable bytes** — a malformed
`.pst` must never crash the process. Native `go test -fuzz` targets over every
public entry point that parses bytes (`New`, `WalkFolders`, `MessageIterator`,
the attachment/recipient/body getters) are the **independent, non-LLM oracle**:
a fuzzer's crashes can't share the blind spots of whoever wrote the code.

- Seed corpora live under `pkg/testdata/fuzz/<FuzzName>/`. A crash the fuzzer
  finds gets written there and becomes a **committed regression case** that the
  plain `go test` run replays — so fixing a fuzz crash means committing both the
  fix and the reproducer.
- Run one locally, time-boxed:
  `go test -run='^$' -fuzz='^FuzzNew$' -fuzztime=30s ./pkg`
- Fuzzing is too slow to gate every PR — run it in a nightly/manual job, not
  per-PR.

## Architecture (bottom-up)

The format is layered; the code mirrors the layers. Read in this order when
tracing a bug:

1. **NDB (Node Database) — raw file & B-trees.** `file.go` (signature, format
   type, encryption, the `Reader` abstraction), `btree.go` (node & block
   B-trees), `blocks.go`, `local_descriptors.go` (subnode data for large
   properties).
2. **Heap-on-Node.** `heap_on_node.go`, `heap_on_node_reader.go` — the heap
   allocator that sits inside a single node's data.
3. **LTP (Lists, Tables, Properties).** `property_context.go` (a node's property
   bag), `table_context.go` (row/column tables, e.g. the folder's message
   table), `property_reader.go`, `property.go`, `name_to_id_map.go` (named-property
   → ID resolution).
4. **Messaging objects.** `message_store.go`, `folder.go` (folder tree +
   `WalkFolders`), `message.go` (`GetMessage`, `MessageIterator`, body getters),
   `attachment.go`, `recipient.go`, `sender.go`.
5. **Generated property structs.** `pkg/properties/` (see below).

Cross-cutting: `errors.go` (sentinel errors), `charsets.go` / `codepage.go`
(ANSI/code-page decoding), `rtf.go` (compressed-RTF), `io_uring.go` (Linux async).

## Gotchas (intrinsic behavior, not bugs to "fix")

- **Not concurrency-safe.** `File`, its `Reader`, and `MessageIterator` carry no
  synchronization — drive a single archive from one goroutine. The established
  pattern is: walk + extract on one goroutine, copy out plain values, then fan
  out the downstream work. (Parallelizing the walk itself is a separate, larger
  change — see upstream issue #28.)
- **`GetSubject()` returns the raw `PidTagSubject`**, including Outlook's leading
  control-character prefix (a `0x01` marker + length byte encoding the RE:/FW:
  split). Strip leading control runes if you need a display subject, or use
  `GetNormalizedSubject()` (RE:/FW: already removed).

## Generated code — do not hand-edit

`pkg/properties/*.go` is generated; edits get overwritten on regeneration.

- `*.pb.go` — protobuf structs, generated from `cmd/properties/protobufs/*.proto`
  by `cmd/properties/generate.go`.
- `*.pb_gen.go` — `msgp` (MessagePack) codecs, from the `//go:generate msgp
  -tests=false` directive at the top of each `*.pb.go`.
- `cmd/properties/` is a **separate Go module** (its own `go.mod`); regenerate
  from there. `properties.csv` is the property-ID map.

To expose a property that isn't on a generated struct, add a hand-written
accessor in `pkg/` (see "fork conventions") rather than editing the generated
files.

## Style

- **`gofmt` / `goimports` clean, `go vet` clean.** Run before every commit.
- **Errors as values, never `panic` in code we write.** Public entry points
  return `error`. Wrap at each layer boundary with `eris.Wrap` / `eris.Wrapf`
  (`github.com/rotisserie/eris`); use the sentinel errors in `errors.go` for
  conditions callers branch on. Some older files use `github.com/pkg/errors` —
  match the file you're editing. Code we *transitively call* (and our own older
  parse paths) can still panic on malformed bytes, so every public boundary that
  crosses into a parser should `recover()` and turn the panic into a returned
  error rather than a process crash — that's the issue #1 P3 work, and the model
  for any new boundary.
- **No stdout/stderr from the library.** Do not add `fmt.Print*` anywhere under
  `pkg/`. A library must stay silent; surface information through return values,
  errors, or an **opt-in logger interface that defaults to no-op**.
- **Prefer explicit dependencies over package-level state.** Pass the reader,
  config, and (when added) logger explicitly; avoid new package-level mutable
  globals and `init()` side effects. (The existing `ExtendCharsets` global is a
  legacy exception, not a pattern to copy.)
- **A single implementation is a struct, not an interface.** Add an interface
  only when there's a second implementation or a genuine seam to test against —
  the way `Reader` exists because `DefaultReader` and `AsyncReader` both
  implement it. Don't introduce speculative interfaces.
- **License header.** Every `.go` file starts with the Apache-2.0 header block
  (`Copyright 2023 Marten Mooij`). Copy it verbatim into new files.
- **Fork accessors live in their own files.** New first-class API added by this
  fork goes in dedicated files (e.g. `sender.go` for SMTP accessors, the
  `GetBodyHTML` helper in `message.go`, ANSI decoding in `charsets.go` /
  `codepage.go`) — keeping the diff against upstream legible and avoiding edits
  to generated structs.

## Git & PR workflow

- **All changes via PR; never commit to `main`.** Confirm you're on a feature
  branch before staging.
- **Branch prefixes:** `feat/`, `fix/`, `docs/`, `test/`, `refactor/`, `chore/`.
- **One concern per branch.** If a task grows a second concern, cut a second
  branch.
- **No PR stacking — always branch from `main`.** If a follow-up depends on an
  in-flight PR, ship a degraded-but-correct version that activates when the base
  lands, rather than stacking.
- **Before pushing follow-up commits, check PR state**
  (`gh pr list --head <branch> --state all --repo Private-Recall/go-pst`). If the
  PR already merged or closed, cut a new branch.
- **Expect `main` to move under you.** Before opening a PR, `git fetch origin
  main`; if it moved, merge and resolve *before* opening rather than discovering
  a dirty mergeable state after. Keep changes tightly scoped so the conflict
  surface stays small. Before starting a numbered issue, check it isn't already
  in progress; before creating a new standalone tool/doc, scan in-flight and
  merged work for a duplicate.
- **Worktree hygiene** (this session runs in `.claude/worktrees/<name>`):
  - A `cd` to the repo root can land in the **main checkout** (the worktree's
    parent), which may be on a sibling session's branch with uncommitted work.
    Before staging, run `git branch --show-current` and `git status` to confirm
    where you are.
  - `git add` **only the specific files you created/changed** — never `git add
    -A` or `git commit -am`, which can sweep an unrelated dirty file onto the
    wrong branch.
  - Prefer doing isolated work inside your own worktree, cut from freshly
    fetched `origin/main`. When something looks "already fixed" or
    "can't reproduce," suspect a stale worktree first: compare
    `git rev-parse HEAD` against `git rev-parse origin/main` (fetch first).
  - At session end, remove worktrees you created and delete their merged
    branches; leave other sessions' worktrees alone (they may hold uncommitted
    work).
- **Auto-merge:** if the repo has native auto-merge enabled, don't arm
  `gh pr merge --auto` on a PR you're still iterating this session — it fires on
  the first green commit and strands your next push. This bites hardest on
  docs-only PRs (no compile/test gate). Arm it only on the final version.

## When to stop and ask

Stop and ask rather than guess when:

- A change would weaken a load-bearing invariant (panic-safety, the
  silent-library rule, a public API contract a consumer relies on).
- A design call isn't covered by anything here and has non-trivial consequences
  if wrong.
- A fix would diverge the fork's public API from what upstream/consumers expect
  without a clear migration.

Guessing in silence on a load-bearing decision costs more than pausing.

## Versioning & release

- Tags follow `v6.0.<N>-recall.<M>` (latest: `v6.0.6-recall.1`).
- After a public API change: bump the tag, then update the `replace` and require
  in recall's `go.mod` to the new tag.

## Working with GitHub here

- `gh` defaults to the **upstream** `mooijtech/go-pst`. For this fork's issues
  and PRs, pass `--repo Private-Recall/go-pst` explicitly.
- Prefer the REST API (`gh api ...`) over wrapper subcommands (`gh pr create`,
  etc.) — see the user's global instructions for the rationale (GraphQL rate-limit
  bucket).

## Active hardening (issue #1)

[Issue #1](https://github.com/Private-Recall/go-pst/issues/1) tracks making the
parser robust for all consumers: prefix-based/case-insensitive message-class
routing, exposing `Message.Class` + `Message.Kind`, removing stdout prints,
recover→error at every public boundary, a crisper `MessageIterator` contract,
and ANSI/Unicode test + fuzz coverage. Keep new work consistent with that
direction (silent library, no panics across the public API).
