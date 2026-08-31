# AGENTS.md

Guidance for AI agents working in this repo. Read this first; it explains the
layout, the conventions, and how GitHub ticket/issue work is done here.

## Project overview

`comfyctl` is a small Go CLI for viewing, modifying, and submitting
[ComfyUI](https://github.com/comfyanonymous/ComfyUI) **API-format** workflows.
It is a unix-style filter: for `dump`/`set`/`submit` you pipe a workflow in on
stdin and get the result on stdout, so commands compose:

```sh
cat wf.json | comfyctl dump
cat wf.json | comfyctl set seed random | comfyctl submit -o ./out
```

No external dependencies beyond the Go standard library. Module
`github.com/tkdlabs/comfyctl`, Go 1.26.4 (see `go.mod`). README.md is the
user-facing docs; IMPROVEMENTS.md is an internal findings/roadmap document
that issues frequently reference.

## Commands and where the code lives

| Command | Behavior | File |
| --- | --- | --- |
| `dump [what...]` | Print role values found in the workflow (all roles if no args). Stdin -> stdout. | `dump.go` |
| `set <what> <val>` | Change a role's value everywhere it matches; `seed random` allowed for int roles. Stdin -> stdout. | `set.go` |
| `mark [-i file] [-f] [role] [ref]` | Pin a node input to a (possibly custom) role, persisted in the node's `_meta.comfyctl`. Interactive unless `ref` (`node:input`) given. `-i` edits a file in place; default is stdin -> stdout. `-f/--overwrite` moves/replaces existing markers. | `mark.go` |
| `submit [flags]` | POST the workflow to ComfyUI `/prompt`, poll `/history`, download outputs from `/view`. Flags: `--host`, `-o/--out`, `--prefix`, `--no-download`, `--include-temp`, `--timeout`. | `submit.go` |

Supporting code:

- `main.go` — command dispatch + top-level usage text.
- `finder.go` — fuzzy role-search heuristics (`findByRole`, `FindSeed`,
  `FindPositivePrompt`, ...). The inverted-crawl rewrite (issue #4) made it the
  tool's resilient heart: finders follow *any* upstream node-ref, bounded by
  depth, per-role hints, and `bannedClasses`. Transfer rules and design
  boundaries live in IMPROVEMENTS.md "Architectural note" and #2.
- `workflow.go` — the `ComfyWorkflow` model: parse, `ResolveRole`,
  `SetString`/`SetInt`, marker read/write (`MarkRole`, `ClearMark`,
  `FindAllMarkedRoles`), `WriteOut`.
- `parser.go` — API-vs-GUI format detection, node/input parsing. Uses
  `json.Decoder.UseNumber()` so int64 seeds > 2^53 survive a round-trip.
- `json_tools.go` — small `extractMap`/`extractArray`/`extractString` helpers.
- `harness_test.go` — the test harness over `testdata/` (see Testing).
- `testdata/` — 26+ vanilla ComfyUI API workflows; the compliance corpus.
- `IMPROVEMENTS.md` — known heuristic gaps, design stances, and the markers
  design. Cross-reference it whenever working on finder/mark behavior.

## Roles

Built-in roles: `positive`, `negative`, `width`, `height`, `fps`, `image`,
`batch`, `seed`. Custom roles (any string) are user-defined via `mark` and
stored per-file in each node's `_meta.comfyctl` (`{"role": ..., "input": ...}`).
`ResolveRole` honors markers first, then falls back to fuzzy search. One
marker per node, one place per role — `mark` refuses to clobber without `-f`.

## Build and verification

Run all of these before finishing any change:

```sh
go build ./...
go vet ./...
gofmt -l .          # must print nothing
go test ./...
```

`harness_test.go` also prints a compliance matrix under `go test -v`. Don't
mutate files in `testdata/` from tests — copy to a temp dir first (see
`copyWorkflowToTemp`).

## Conventions

- Unix filter style as above; keep commands composable via stdin/stdout.
- Errors are returned up to `main.go`, which prints them to stderr with an
  `error:` prefix. Progress/notes go to stderr too; stdout carries data only.
- Preserve int64 precision: handle JSON numbers via `json.Number`, never float.
- No comments unless they earn their keep; match existing style.
- Commit messages: short imperative phrases, optional semicolon-separated
  parts, e.g. `Fix typos, stable seed ordering, and mark overwrite protection`.
  Do not commit or push unless the user asks.

## GitHub / ticket handling

- Remote: `https://github.com/tkdlabs/comfyctl` (`tkdlabs/comfyctl`), branch
  `main`. `gh` CLI is authenticated (also used as the git credential helper).
- CI (`.github/workflows/ci.yml`) runs `go build`, `go vet`, `go test ./...`
  on every push to `main` and on PRs.
- Releases are tagged `vN.M.N` (current: `v0.1.0`, no attached binaries yet —
  see issue #3).
- To file tickets use `gh issue create --repo tkdlabs/comfyctl`. Existing
  issues #1–#4 track the referenced improvements; they cite IMPROVEMENTS.md.
- When you fix a ticketed item, reference the ticket in the commit message
  (e.g. `Fix #2: ...`) so GitHub links/closes it, then verify the CI run on
  the pushed `main` passes (`gh run watch <id> --exit-status`).
- Prefer committing one logical change per commit; push only after the build,
  vet, gofmt, and tests are green.

Note: `AGENTS.md` is read at session start — restart opencode if it changes
and you want the new guidance picked up immediately.