# Comfyctl

A command line tool for viewing, modifying, and submitting [ComfyUI](https://github.com/comfyanonymous/ComfyUI)
API workflows from the shell.

Comfyctl reads a workflow in ComfyUI's **API format** (the JSON produced by
"Save (API Format)" in the web UI), lets you inspect and tweak the interesting
bits — prompt, seed, resolution, batch size, input image — and submits it
straight to a ComfyUI server, downloading the generated files when the run
finishes.

## Install

```sh
go install github.com/tkdlabs/comfyctl@latest
```

## Usage

Comfyctl is a unix-style filter: most commands read a workflow from **stdin**
and write the updated workflow to **stdout**, so they compose with pipes.

```sh
cat workflow.json | comfyctl dump
cat workflow.json | comfyctl set seed random | comfyctl submit -o ./out
```

### dump

Best-effort inspection of the workflow's key attributes. With no arguments it
reports everything it can find; otherwise it prints only the requested roles.
Multiple roles can be supplied.

```sh
comfyctl dump            # all roles
comfyctl dump seed width # just seed and width
```

Supported roles: `positive`, `negative`, `width`, `height`, `fps`, `image`,
`batch`, `seed` — plus any [custom roles](#how-roles-work) you have marked.

### set

Changes a workflow attribute and writes the updated workflow to stdout.
Applies the new value to **every** matching node (e.g. `set seed` updates all
samplers).

```sh
cat workflow.json | comfyctl set positive "A knight facing a dragon"
cat workflow.json | comfyctl set seed 12345
cat workflow.json | comfyctl set seed random
cat workflow.json | comfyctl set width 1024 | comfyctl set height 1024
```

Integer roles (`width`, `height`, `fps`, `batch`, `seed`) accept `random` to
pick a random value. If a role can't be found, run `comfyctl dump` first and
consider [marking](#how-roles-work) it manually.

### mark

Manually associates a node input with a role. The fuzzy search used by
`dump`/`set` doesn't always succeed (new node types, bespoke input names,
fan-in like concatenated prompts) — `mark` pins the mapping by hand, persisted
in the node's `_meta.comfyctl`. Once marked, `set`/`dump` prefer the marker
over fuzzy search.

Without a ref it runs interactively: it lists every markable input and asks
you to pick one. Provide a `node:input` ref to skip the prompt:

```sh
comfyctl mark positive 116:42:value        # mark a specific input
comfyctl mark character_image 3:LoadImage  # custom role
comfyctl mark positive -i workflow.json    # edit a file in place
comfyctl mark -d character_image -i wf.json  # delete a marker, back to fuzzy
```

`mark` is the only command that can edit files in place; otherwise it reads
stdin and writes stdout like the rest.

Deleting a marker with `mark -d <role>` returns that node to heuristic
(fuzzy) resolution. Because a role should live in exactly one place, `mark`
refuses to re-mark a role already pinned elsewhere (or to put a second marker
on a node) unless you pass `-f` to move it. `dump` surfaces any stray
violation — a role marked on multiple nodes, or a marker pointing at a missing
input — as a note, and `mark -d` is the way to clear them before re-marking.

When you mark a role that is neither built-in nor already used in the file,
`mark` prints a warning — a guard against a typo (e.g. `charater_image`)
silently creating a phantom role.

### roles

Lists every role marked via `mark`, with node counts, so you can see a
workflow's marker registry at a glance (the file's `_meta.comfyctl` is the
only source of truth — there is no global config).

```sh
cat workflow.json | comfyctl roles
```

Output, one role per line with its node id(s):

```
bg_audio        (1 node)       16
character_image (1 node)       11
```

As with `dump`, any uniqueness violation — a role marked on multiple nodes or
a marker pointing at a missing input — is printed as a `Note:` so you can fix
it with `mark -d <role>` and re-mark.

### submit

Posts a workflow to ComfyUI, waits for it to complete, and downloads the
output files.

```sh
cat workflow.json | comfyctl submit
cat workflow.json | comfyctl submit --host http://127.0.0.1:8188 -o ./out --prefix job1_
cat workflow.json | comfyctl submit --no-download   # just run it
```

| Flag            | Description                                        | Default            |
|-----------------|----------------------------------------------------|--------------------|
| `--host`        | ComfyUI server address                             | `http://127.0.0.1:8188` |
| `-o, --out`     | Directory for downloaded outputs                   | `.`                |
| `--prefix`      | Prefix prepended to saved output filenames         | (empty)            |
| `--no-download` | Submit only; print prompt id and exit              | off                |
| `--include-temp`| Also download `temp` outputs (previews)            | off                |
| `--timeout`     | Max time to wait for completion, e.g. `10m`, `1h`  | `10m0s`            |

## Examples

```sh
# Inspect a workflow
cat video_ltx2_3_t2v.json | comfyctl dump

# Re-roll the same workflow with a new prompt and seed
cat wf.json \
  | comfyctl set positive "A robot painter in a loft studio" \
  | comfyctl set seed random \
  | comfyctl submit -o ./out

# Edit an existing file in place by hand-pinning a role
comfyctl mark positive 116:42:value -i wf.json
```

### version

Prints the version and build metadata (injected at release time via
goreleaser `-ldflags`). A locally built binary reports the `dev` defaults.

```sh
comfyctl version
comfyctl --version
# comfyctl v0.2.0 (commit: 1cd4376, built: 2026-08-30T10:00:00Z)
```

## How roles work

Roles are names for the attributes you care about. The built-in roles
(`positive`, `negative`, `width`, `height`, `fps`, `image`, `batch`, `seed`)
are found via fuzzy search. `mark` lets you define **custom roles** and pin
built-in ones when the search guesses wrong.

Markers are stored **per-file** in each node's `_meta.comfyctl` map — no
global config. The workflow is self-describing: ComfyUI ignores the extra
`_meta.comfyctl` key, so marked files submit and generate normally.

## Development

```sh
go build ./...
go test ./...   # runs the testdata/ harness over sample workflows
```

### Releases

Releases are built with [goreleaser](https://goreleaser.com) and attached to
GitHub releases by the `.github/workflows/release.yml` workflow, which runs
when a `v*` tag is pushed:

```sh
git tag v0.2.0
git push origin v0.2.0
```

The pipeline builds `linux`/`darwin`/`windows` on `amd64`/`arm64`, injects
version/commit/date via `-ldflags`, and uploads archives + checksums to the
release. Validate the config locally with `goreleaser check` (or a snapshot
build via `goreleaser release --snapshot --skip=publish --clean`).

## License

[MIT](LICENSE)