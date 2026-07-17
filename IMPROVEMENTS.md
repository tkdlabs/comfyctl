# Improvements

Findings from running the `testdata/` harness (`go test ./...`) over 26 vanilla
ComfyUI API workflows. All 26 **parse** cleanly; every gap below is in the
finder heuristics. Root cause is shared: the finders anchor on a fixed
whitelist of input names, and newer templates rename inputs and insert routing
nodes the crawl doesn't follow.

## Prioritized by blast radius

### 1. Seed finder misses `noise_seed` (18 / 26 files)
`RandomNoise` (flux2, hunyuan, ltx2) and `KSamplerAdvanced` (wan2.2,
key-frames) name the input `noise_seed`, not `seed`. `FindSeed` only anchors on
`seed`, so seed detection silently fails on the majority of current workflows —
only legacy `KSampler` and the bytedance API nodes still hit.
- **Fix:** add `noise_seed` to both the anchor check and the crawl follow-list.
- Cheap, highest impact.

### 2. Prompt crawl dead-ends at routing / processing nodes
- **LTX2 t2v/i2v/ia2v — positive lost.** Chain is
  `CFGGuider.positive -> LTXVConditioning -> CLIPTextEncode.text -> ComfySwitchNode`
  (`on_true`/`on_false`/`switch`). `followInputs` lacks those keys, so the crawl
  stops at the Switch. Negative is found only because its `CLIPTextEncode.text`
  is an inline string.
- **krea2 t2i — positive lost entirely.**
  `CLIPTextEncode.text -> StringConcatenate` (`string_a`/`string_b`)
  `-> PrimitiveStringMultiline.value`, plus a `TextGenerate` LLM node. The crawl
  lacks `string_a`/`string_b`, dead-ends.
- **Fix (stopgap):** extend `followInputs` with routing keys.
- **Fix (real):** see architectural note below.

### 3. Titled-node branch bails on a node-ref and spams stderr
In `image_flux2_klein_text_to_image.json` the node titled
`CLIP Text Encode (Positive Prompt)` has `text` = a *ref* to a `PrimitiveString`,
not an inline string. The fuzzy-title branch requires `ComfyTextInput`, hits
`log.Printf("Weird, node with input 'text' is not string...")` on stderr, and
gives up. The fallback saves it, but every run leaks the log line.
- **Fix:** when the titled node's `text` is a `ComfyNodeRef`, crawl into it
  instead of bailing; drop the log or gate it behind a verbose flag.

### 4. Cloud / API nodes use node-specific field names
`ByteDanceTextToVideoNode` has a direct `prompt` input (no `positive`, no
`CLIPTextEncode` anchor); `api_wan2_7_i2v` has none of `positive`/`prompt`/title.
These can't be found at all.
- **Fix:** add `prompt` as a positive anchor for a free partial win; full
  support likely needs a class_type -> field mapping. Lower priority (API
  workflows are a distinct submode).

### 5. `set` updates only one node; multi-seed workflows go half-updated
An attribute can map to N nodes, but `Find*` returns a single `InputRef` and
`set` writes exactly one. `set seed X` on key-frames updates 1 of 5 enabled
samplers (the other 4 keep the old seed); on LTX2 it updates 1 of 2 `RandomNoise`
nodes. Worse, which node is picked is nondeterministic (map iteration order), and
`dump seed` reads stable on key-frames — so the write silently corrupts while the
read looks fine. Guarded by `TestSetSeedUpdatesAllNodes` (currently skipped).
- **Fix:** `Find*` returns `[]InputRef`; `set` applies to all matches; `dump`
  reports the common value and warns when enabled matches diverge. This is the
  concrete form of the architectural note below.

### 6. Lock in the non-bugs with golden assertions
- Empty `negative=""` on flux-klein base models is genuine (empty conditioning),
  not a miss.
- Colon-namespaced node IDs (`98:22`) parse fine.
- Add assertions so neither silently regresses.

## Architectural note

Items 1–4 are one fragility: a whitelist of input names can't keep up with new
templates. Every generation adds routing nodes (Switch, Concatenate, primitive
value nodes) that must be chased by hand.

Invert the crawl: follow *any* upstream node-ref, stop at the first node whose
input matches the target **type** and whose class isn't banned, bounded by
depth. This survives new node types without edits. The real risk is
**ambiguity** — krea2 has two `PrimitiveStringMultiline` nodes (system prompt +
user prompt), so "follow everything" could grab the wrong one. Mitigate by
ranking: prefer known classes (`CLIPTextEncode`, `Primitive*`) and keep the
`bannedClasses` escape hatch for system-prompt / negative nodes.

The whitelist patches (items 1, 4) are fine as a stopgap to get seeds working
today; the inverted crawl is the version worth building.
