package main

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
)

func findByRole(cw ComfyWorkflow, role string) ([]InputRef, error) {
	switch role {
	case "seed":
		return FindSeed(cw)
	case "positive":
		return one(FindPositivePrompt(cw))
	case "negative":
		return one(FindNegativePrompt(cw))
	case "width":
		return one(FindWidth(cw))
	case "height":
		return one(FindHeight(cw))
	case "fps":
		return one(FindFps(cw))
	case "image":
		return one(FindImage(cw))
	case "batch":
		return one(FindBatchSize(cw))
	default:
		return nil, fmt.Errorf("unknown role: %s", role)
	}
}

func one(ref InputRef, err error) ([]InputRef, error) {
	if err != nil {
		return nil, err
	}
	return []InputRef{ref}, nil
}

// findScalarRole is the shared Number-role finder for width/height/fps/batch.
// Deterministic by construction: immediate scalar anchors (an input literally
// named `key` holding a number) win over crawled ref-anchors, and both passes
// iterate node ids in sorted order so outcomes never depend on map order.
func findScalarRole(workflow ComfyWorkflow, key, label string) (InputRef, error) {
	for _, k := range sortedNodeIDs(workflow) {
		v, found := workflow.Nodes[k].Inputs[key]
		if !found {
			continue
		}
		if v.Type == ComfyNumberInput {
			return InputRef{nodeId: k, inputId: key, inputType: ComfyNumberInput}, nil
		}
	}
	for _, k := range sortedNodeIDs(workflow) {
		if _, found := workflow.Nodes[k].Inputs[key]; !found {
			continue
		}
		if ref, found := crawlUntilFound(workflow, k, ComfyNumberInput, []string{key, "value"}, nil); found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find " + label + " in the workflow")
}

func FindHeight(workflow ComfyWorkflow) (InputRef, error) {
	return findScalarRole(workflow, "height", "height")
}

func FindWidth(workflow ComfyWorkflow) (InputRef, error) {
	return findScalarRole(workflow, "width", "width")
}

func FindBatchSize(workflow ComfyWorkflow) (InputRef, error) {
	return findScalarRole(workflow, "batch_size", "batch_size")
}

func FindFps(workflow ComfyWorkflow) (InputRef, error) {
	return findScalarRole(workflow, "fps", "fps")
}

func FindSeed(workflow ComfyWorkflow) ([]InputRef, error) {
	refsfound := make([]InputRef, 0)
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["seed"]
		if found {
			ref, found := crawlUntilFound(workflow, k, ComfyNumberInput, []string{"seed", "value"}, nil)
			if found {
				refsfound = append(refsfound, ref)
			}
		}
		// try with noise_seed.
		_, found = node.Inputs["noise_seed"]
		if found {
			addNoiseVal, found := node.Inputs["add_noise"]
			if !found || addNoiseVal.Text == "enable" {
				ref, found := crawlUntilFound(workflow, k, ComfyNumberInput, []string{"noise_seed", "seed", "value"}, nil)
				if found {
					refsfound = append(refsfound, ref)
				}
			}
		}
	}
	if len(refsfound) == 0 {
		return refsfound, errors.New("Unable to find seed in the workflow")
	}
	sort.Slice(refsfound, func(i, j int) bool { return refsfound[i].nodeId < refsfound[j].nodeId })
	if len(refsfound) > 1 {
		log.Printf("Note: Found %d potential seed locations: [%v]", len(refsfound), refsfound)
	}
	return refsfound, nil
}

// promptHints / negativeHints are the role words a prompt crawl treats as
// semantic continuations: tried before arbitrary refs on a node, and the only
// names that qualify as terminal scalar inputs upstream. The two sets are
// deliberately disjoint on the polarity word, so a crawl that follows
// `positive` never bleeds into a `negative` branch at a conditioning junction
// (and vice versa) — see the InpaintModelConditioning / krea2 cases.
var promptHints = []string{"value", "text", "positive", "prompt", "conditioning", "on_true", "on_false"}
var negativeHints = []string{"value", "text", "negative", "prompt", "conditioning", "on_true", "on_false"}

// promptBannedClasses never enter the text crawl: ConditioningZeroOut (an
// inert/negative conditioning) and the string fan-in merges, which must stay
// marker-resolved rather than heuristic-guessed ("crawl through routing, mark
// through merges").
var promptBannedClasses = []string{"ConditioningZeroOut", "StringConcatenate", "StringConcatenateMulti"}

func FindPositivePrompt(workflow ComfyWorkflow) (InputRef, error) {

	// Fuzzy search for node that has "Positive Prompt" in title
	for _, k := range sortedNodeIDs(workflow) {
		node := workflow.Nodes[k]
		if strings.Contains(node.Title, "Positive Prompt") {
			nodeInput, found := node.Inputs["text"]
			if !found {
				continue
			}
			if nodeInput.Type != ComfyTextInput {
				continue
			}
			return InputRef{nodeId: k, inputId: "text", inputType: ComfyTextInput}, nil
		}
	}

	// Look for inputs "positive" and walk back to "text"
	for _, k := range sortedNodeIDs(workflow) {
		node := workflow.Nodes[k]
		if !any_found(node.Inputs, "positive", "prompt") {
			continue
		}
		ref, found := crawlUntilFound(workflow, k, ComfyTextInput, promptHints, promptBannedClasses)
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find positive prompt in the workflow")
}

func any_found(inputs map[string]ComfyNodeInput, keys ...string) bool {
	for _, key := range keys {
		_, found := inputs[key]
		if found {
			return true
		}
	}
	return false
}

func FindNegativePrompt(workflow ComfyWorkflow) (InputRef, error) {

	// Fuzzy search for node that has "Negative Prompt" in title
	for _, k := range sortedNodeIDs(workflow) {
		node := workflow.Nodes[k]
		if strings.Contains(node.Title, "Negative Prompt") {
			nodeInput, found := node.Inputs["text"]
			if !found {
				continue
			}
			if nodeInput.Type != ComfyTextInput {
				continue
			}
			return InputRef{nodeId: k, inputId: "text", inputType: ComfyTextInput}, nil
		}
	}

	// Look for inputs "negative" and walk back to "text"
	for _, k := range sortedNodeIDs(workflow) {
		node := workflow.Nodes[k]
		_, found := node.Inputs["negative"]
		if !found {
			continue
		}
		ref, found := crawlUntilFound(workflow, k, ComfyTextInput, negativeHints, promptBannedClasses)
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find negative prompt in the workflow")
}

func FindImage(workflow ComfyWorkflow) (InputRef, error) {
	for _, k := range sortedNodeIDs(workflow) {
		node := workflow.Nodes[k]
		if node.ClassType == "LoadImage" {
			nodeInput, found := node.Inputs["image"]
			if !found || nodeInput.Type != ComfyTextInput {
				continue
			}
			return InputRef{nodeId: k, inputId: "image", inputType: ComfyTextInput}, nil
		}
	}
	return InputRef{}, errors.New("Unable to find source image in the workflow")
}

func FindAllNonRefInputs(workflow ComfyWorkflow) []InputRef {
	var res []InputRef
	for k, node := range workflow.Nodes {
		for ki, input := range node.Inputs {
			if input.Type != UnknownNodeInputType && input.Type != ComfyNodeRef {
				res = append(res, InputRef{k, ki, input.Type})
			}
		}
	}
	return res
}

// maxCrawlDepth caps how far the inverted crawl will chase upstream refs. Deep
// enough for real pipelines (LTX2 positive is ~4 hops, the krea2 switch chain
// is ~5) without letting a pathological graph balloon forever.
const maxCrawlDepth = 10

// sortedNodeIDs returns the workflow's node ids in sorted order, so finder
// outcomes never depend on map iteration order.
func sortedNodeIDs(workflow ComfyWorkflow) []string {
	ids := make([]string, 0, len(workflow.Nodes))
	for id := range workflow.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// crawlUntilFound is the inverted crawl (issue #4): instead of a hand-maintained
// list of input names to follow, it walks *any* upstream node-ref and stops at
// the first node whose input matches the target type, bounded by depth and by
// bannedClasses. Routing nodes with arbitrary input names (ComfyMathExpression
// `values.*`, switch arms, ...) are chased without edits; new node types
// survive by construction.
//
// Three ordering rules keep this deterministic and free of the two failure
// modes a naive "follow everything" has:
//   - Terminal scalar inputs must be named by a hint (a role word like
//     `text`/`positive`/`value`), so a node's incidental parameters (`steps`,
//     `sampling_mode`, a checkpoint path) never qualify.
//   - When a node has ref inputs that match a hint, only those are chased —
//     at a conditioning junction that keeps `positive` from bleeding into the
//     `negative` branch, and keeps `negative` from falling back to the
//     positive text when its own branch dead-ends.
//   - A generic hop (following a non-hint ref on a routing node like a math
//     expression) may never chain into another generic hop: it must land on a
//     terminal or on a node with hint-matched refs. Otherwise a primitive
//     buried in the model graph (a "Steps" value, megapixels, a checkpoint
//     name) gets mistaken for the role's value.
func crawlUntilFound(workflow ComfyWorkflow, startNode string, targetType ComfyNodeInputType, hints, bannedClasses []string) (InputRef, bool) {
	return crawlUntilFoundDepth(workflow, startNode, targetType, hints, bannedClasses, make(map[string]bool), 0, false)
}

func crawlUntilFoundDepth(workflow ComfyWorkflow, nodeId string, targetType ComfyNodeInputType,
	hints, bannedClasses []string, visited map[string]bool, depth int, generic bool) (InputRef, bool) {
	invalidRes := InputRef{}
	if depth > maxCrawlDepth {
		return invalidRes, false
	}
	node, found := workflow.Nodes[nodeId]
	if !found {
		return invalidRes, false
	}
	if slices.ContainsFunc(bannedClasses, func(class string) bool { return class == node.ClassType }) {
		// don't follow nodes of 'banned classes'
		return invalidRes, false
	}
	if visited[nodeId] {
		return invalidRes, false
	}
	visited[nodeId] = true
	defer delete(visited, nodeId)

	names := make([]string, 0, len(node.Inputs))
	for name := range node.Inputs {
		names = append(names, name)
	}
	sort.Strings(names)

	// Terminal: an input of the target type whose name is a role hint.
	var hintedRefs []string
	for _, name := range names {
		input := node.Inputs[name]
		if input.Type == targetType && slices.Contains(hints, name) {
			return InputRef{nodeId: nodeId, inputId: name, inputType: targetType}, true
		}
		if input.Type == ComfyNodeRef && slices.Contains(hints, name) {
			hintedRefs = append(hintedRefs, name)
		}
	}

	// Semantic continuation: a node whose inputs carry role words is chased
	// only through those — never through unrelated refs, even if a hinted one
	// dead-ends (that's what keeps `negative` from reusing the positive text).
	if len(hintedRefs) > 0 {
		sort.Strings(hintedRefs)
		for _, name := range hintedRefs {
			if ref, found := crawlUntilFoundDepth(workflow, node.Inputs[name].OutputPtr.NodeRef,
				targetType, hints, bannedClasses, visited, depth+1, false); found {
				return ref, true
			}
		}
		return invalidRes, false
	}

	// Generic hop: only on routing/primitives with no role-named inputs, one
	// hop maximum, and only from a non-generic entry.
	if generic {
		return invalidRes, false
	}
	var refs []string
	for _, name := range names {
		if node.Inputs[name].Type == ComfyNodeRef {
			refs = append(refs, name)
		}
	}
	sort.Strings(refs)
	for _, name := range refs {
		if ref, found := crawlUntilFoundDepth(workflow, node.Inputs[name].OutputPtr.NodeRef,
			targetType, hints, bannedClasses, visited, depth+1, true); found {
			return ref, true
		}
	}
	return invalidRes, false
}
