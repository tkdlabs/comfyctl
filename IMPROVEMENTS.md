# Improvements

Findings from running the `testdata/` harness (`go test ./...`) over 26 vanilla
ComfyUI API workflows. All 26 **parse** cleanly. The finder heuristics below
describe the history of the largest gap class — a whitelist of input names and
a hand-maintained follow list that could not keep up with new templates. That
fragility is now resolved by the **inverted crawl** (see "Architectural note"
and issue #4): finders walk *any* upstream ref, stop at the first input of the
target type, bounded by depth, hints, and banned classes. Items 1–5 record the
original whitelist-era findings and their fixes; the crawl rewrite is #6.

## Prioritized by blast radius

### 1. Seed finder misses `noise_seed` (18 / 26 files) — DONE (70cc020)
`RandomNoise` (flux2, hunyuan, ltx2) and `KSamplerAdvanced` (wan2.2,
key-frames) name the input `noise_seed`, not `seed`. `FindSeed` only anchored on
`seed`, so seed detection silently fails on the majority of current workflows —
only legacy `KSampler` and the bytedance API nodes still hit.
- **Fix:** add `noise_seed` to both the anchor check and the crawl follow-list,
  guarded by an `add_noise == "enable"` check so disabled samplers are skipped.
- Coverage now 26/26.

### 2. Prompt crawl dead-ends at routing / processing nodes
Boundary that resolves this class: **crawl through *routing*, mark through
*merges*.** A pass-through node (Switch, Reroute, single-input conditioning
chain) still has exactly one semantic source upstream, so following it is safe
and heuristic-correct. A fan-in node (Concatenate, multiple primitives) is where
semantics genuinely branch — no reliable signal picks the right source, so
guessing risks *silently* writing the wrong input. Resolve those by marker.
The inverted crawl (issue #4) now follows routing nodes *by construction* (any
ref, one bounded generic hop), so new routing nodes stop needing hand-chasing —
and the fan-in merge classes (`StringConcatenate*`) are banned wholesale, so
the "mark through merges" half of the boundary survives.

- **LTX2 t2v/i2v/ia2v — positive lost. DONE.** Chain was
  `CFGGuider.positive -> LTXVConditioning -> CLIPTextEncode.text -> ComfySwitchNode`
  (`on_true`/`on_false`/`switch`). `followInputs` lacked those keys, so the crawl
  stopped at the Switch. Fixed by adding `on_true`/`on_false` to the positive
  crawl — a pure routing pass-through, exactly the safe case. All three LTX2
  workflows now resolve `positive`.
- **krea2 t2i — positive now resolves via the inverted crawl; system prompt is
  safe. DONE (issue #4).**
  `CLIPTextEncode.text -> ComfySwitchNode` fans into `StringConcatenate`
  (`string_a`/`string_b`) merging a `PrimitiveStringMultiline` system prompt +
  user prompt, with a `TextGenerate` LLM node in the mix. The naive "follow
  everything" grabs the system prompt; the inverted crawl instead:
  - bans `StringConcatenate`/`StringConcatenateMulti`, so the system prompt is
    unreachable (no silent `set positive` corruption of it),
  - resolves the user prompt through the switch arms to the raw
    `PrimitiveStringMultiline` — `dump positive` and `set positive` now hit the
    real user prompt with no marker required.
  This supersedes the earlier stance ("resolve via markers, do NOT crawl") once
  the crawl could prove it could not reach the system prompt. Markers remain the
  correct tool for any *other* krea2-style ambiguity.

### 3. Titled-node branch bails on a node-ref and spams stderr — DONE (fe6f350)
In `image_flux2_klein_text_to_image.json` the node titled
`CLIP Text Encode (Positive Prompt)` has `text` = a *ref* to a `PrimitiveString`,
not an inline string. The fuzzy-title branch required `ComfyTextInput`, logged
`Weird, node with input 'text' is not string...` to stderr, and gave up.
- **Fix applied:** dropped the log line and let the branch fall through; the
  `positive`/`prompt` fallback crawl resolves it silently. (If the titled node
  ever needs to be *preferred* over the fallback, crawl into the ref there
  instead of continuing — not needed today.)

### 4. Cloud / API nodes use node-specific field names — partial (common case DONE)
- **DONE — `prompt` anchor.** All three ByteDance nodes
  (`ByteDance{TextToVideo,ImageToVideo,FirstLastFrame}Node`) expose a flat
  `prompt` input. Added `prompt` to the positive anchor set (`any_found`), so
  they now resolve — 3 of the 4 API workflows.
- **Bespoke / namespaced fields → mark, don't whitelist.** `api_wan2_7_i2v`
  (`Wan2ImageToVideoApi`) names its fields `model.prompt` / `model.negative_prompt`
  — dotted namespaces, so no flat anchor hits. This is the un-findable tail, and
  chasing it is a losing game: the next provider will be `params.text` /
  `input.caption`, and a `class_type -> field` table only ever covers nodes
  already seen. Do **not** reach for substring matching either — `model.negative_prompt`
  contains `prompt`, so a `contains("prompt")` positive anchor would grab the
  *negative* field. Resolve by marker: these nodes are flat (prompt is a direct
  scalar input on one node, no crawl, no ambiguity), so `mark positive <nodeId>`
  is trivially correct. Same boundary as #2 — anchor the common/stable names,
  mark the bespoke tail.
- **When building the mark path:** negative has the identical story
  (`model.negative_prompt`); and exercise the marked-input round-trip on a
  **dotted key** to confirm the `.` in the input name survives parse -> `set` ->
  write-out (should be fine — it's just a map key — but verify).

### 5. `set` updates only one node; multi-seed workflows go half-updated — DONE (04b132e)
An attribute can map to N nodes, but `Find*` returned a single `InputRef` and
`set` wrote exactly one. `set seed X` on key-frames updated 1 of 5 enabled
samplers (the other 4 kept the old seed); on LTX2 it updated 1 of 2 `RandomNoise`
nodes. Worse, which node was picked was nondeterministic (map iteration order),
and `dump seed` read stable on key-frames — so the write silently corrupted while
the read looked fine. Guarded by `TestSetSeedUpdatesAllNodes` (now enabled).
- **Fix:** `FindSeed` returns `[]InputRef`; `set` applies to all matches.
- **Follow-up:** `FindSeed` still appends in map order, so `dump seed` line
  order flickers run-to-run. Sort `refsfound` by nodeId for stable output
  (matters once `dump` gets golden tests).

### 6. Lock in the non-bugs with golden assertions
- Empty `negative=""` on flux-klein base models is genuine (empty conditioning),
  not a miss.
- Colon-namespaced node IDs (`98:22`) parse fine.
- Add assertions so neither silently regresses.

## Architectural note

Items 1–4 were one fragility: a whitelist of input names can't keep up with new
templates. Every generation adds routing nodes (Switch, Concatenate, primitive
value nodes) that must be chased by hand.

**DONE — inverted crawl (issue #4).** The finder crawl now walks *any* upstream
node-ref and stops at the first node whose input matches the target **type**,
bounded by `maxCrawlDepth`, a hint set per role, and the `bannedClasses` escape
hatch. New routing nodes survive without edits (locked in by
`inverted_crawl_test.go` with fabricated router classes). The ambiguity risk —
krea2's two `PrimitiveStringMultiline` nodes (system + user prompt) — is handled
by banning the fan-in merge classes so the system prompt is unreachable, and
hint-ranking so crawling only follows refs that look like the role. The
whitelist patches (items 1, 4) are historical; the inverted crawl is the
version shipped.

## Roles & markers

`ResolveRole` already resolves markers first, then falls back to `findByRole`.
Building the write path (`mark` command) unlocks user-defined roles, which
generalize the whole finder problem.

### Custom (user-defined) roles
A custom role is a role with **no finder** — the user assigns it by hand via
`mark`, so `markedRefs` returns the nodes and the `findByRole` fallback never
runs. The motivating case: workflows with several image / audio refs (3
`LoadImage` nodes) that the built-in `image` finder can't distinguish. Custom
roles let the user name them (`character_image`, `bg_audio`, `voice_ref`). This
is the same multiplicity problem as seed, solved by the same mechanism.

Marker-first + shared namespace also means `mark seed <node>` can override a
built-in heuristic when it guesses wrong — one mechanism covers "define new
role" and "pin/override an existing one."

Changes needed (once the `mark` write path exists):
1. **Accept any role string** in `set`/`dump`/`mark`; keep `findByRole` as the
   built-in fallback, and treat "unknown role + no markers" as a friendly error
   rather than rejecting the name up front.
2. **Infer `set`'s type from the target input, not a role->type table.** The
   marker points at an input that already has a `ComfyNodeInputType`; branch on
   that. Kills the `isIntRole` string-matching for built-ins too, and is where
   a `SetBool` finally earns its keep (a custom role pointing at a bool
   toggle — the generic `crawlUntilFound` with `ComfyBoolInput` already serves
   the crawl half).
3. **Add a `roles` command** (`comfyctl roles < wf.json`) listing every marked
   role + node count. Discoverability guard against the one footgun: a typo in
   `mark` silently creates a phantom role. **DONE (issue #1)** — `roles` lists
   marked roles with node counts and conflict notes; `mark` warns when a role
   is new to the file.
4. **Overwrite protection on `mark` (one marker per node).** The model stores a
   single `MarkerRole`/`MarkerInput` per `ComfyNode`, and `MarkRole` replaces the
   node's whole `_meta.comfyctl` submap — so marking a second input on a node
   silently overwrites the first, and re-marking a role that already lives on
   another node silently strands the old marker (relevant to the krea2 case in #2
   if user + system prompt share a node). Two clobber paths, same fix:
   - `mark` should **error out** when the target node already has a marker, or
     when `[role]` is already marked elsewhere, **unless** a `-f`/`--overwrite`
     flag is passed. Today `cmdMark` only prints a warning and proceeds.
   - When overwriting a role that lives on a *different* node, call `ClearMark`
     on the old node first, so the role doesn't end up marked in two places.
     (`cmdMark` currently never calls `ClearMark` — this is the missing call.)
   - Longer-term alternative to the per-node ceiling: make `_meta.comfyctl` a
      list of markers instead of a single object, so one node can carry several
      roles. Decide before the format is relied on downstream.
   - **DONE — deletion & reconcile (issue #5).** `mark -d <role>` clears the
      marker everywhere it lives (back to fuzzy), via `ComfyWorkflow.DeleteRole`
      over `markerLocations()`. `DetectMarkerConflicts()` scans the whole file
      for a role marked on multiple nodes or a marker pointing at a missing
      input; `dump` and `roles` print those as notes. Deleting duplicates is
      still an explicit, unambiguous `mark -d` — no auto-fix that could guess
      wrong.
   - **DONE — `roles` command + typo guard (issue #1).** `comfyctl roles`
      lists every marked role with its node count and conflict notes, reading
      the markers back from the file (the only registry). `mark` now warns
      when a role is neither built-in nor already present, catching
      `charater_image`-style typos before they become phantom roles. The
      remaining numbered item is #3 (infer `set`'s type from the target input
      instead of the `isIntRole` table).

### Design stance
Keep roles **schema-less and per-file**: the workflow's `_meta.comfyctl` markers
*are* the registry. No global config declaring roles/types — the file is
self-describing, which fits a tool whose job is "read a workflow, act on it."
The `roles` command reads them back; no external state to keep in sync.

### Round-trip — VERIFIED
The `_meta.comfyctl` round-trip through ComfyUI's `/prompt` endpoint is
confirmed: a marked krea2 workflow submits and generates with the markers
present. ComfyUI ignores the extra `_meta.comfyctl` key rather than rejecting
it, so markers are safe to rely on end-to-end. Exercised on a live host:

    cat image_krea2_turbo_t2i.json \
      | comfyctl set positive "A knight fighting another knight ..." \
      | comfyctl submit --host http://...:8188
    # submitted, prompt id: d40791e7-...; Krea2_turbo_00064_.png

This also closes the krea2 case in #2: the marked prompt overrides the failed
heuristic, `set positive` writes the right side of the fan-in, and the result
submits cleanly. (Since the inverted crawl lands, markers are no longer
*required* for krea2 — `FindPositivePrompt` resolves the user prompt directly —
but the round-trip still proves markers are harmless to ComfyUI and stay the
correct override when a heuristic ever guesses wrong.)
