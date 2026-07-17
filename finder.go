package main

import (
	"errors"
	"log"
	"slices"
	"strings"
)

func FindHeight(workflow ComfyWorkflow) (InputRef, error) {
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["height"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundNumber(workflow, k, []string{"height", "value"}, []string{})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find height in the workflow")

}

func FindWidth(workflow ComfyWorkflow) (InputRef, error) {
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["width"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundNumber(workflow, k, []string{"width", "value"}, []string{})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find width in the workflow")

}

func FindBatchSize(workflow ComfyWorkflow) (InputRef, error) {
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["batch_size"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundNumber(workflow, k, []string{"batch_size", "value"}, []string{})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find batch_size in the workflow")
}

func FindFps(workflow ComfyWorkflow) (InputRef, error) {
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["fps"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundNumber(workflow, k, []string{"fps", "value"}, []string{})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find fps in the workflow")
}

func FindSeed(workflow ComfyWorkflow) (InputRef, error) {
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["seed"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundNumber(workflow, k, []string{"seed", "value"}, []string{})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find seed in the workflow")
}

func FindPositivePrompt(workflow ComfyWorkflow) (InputRef, error) {

	// Fuzzy search for node that has "Positive Prompt" in title
	for k, node := range workflow.Nodes {
		if strings.Contains(node.Title, "Positive Prompt") {
			nodeInput, found := node.Inputs["text"]
			if !found {
				continue
			}
			if nodeInput.Type != ComfyTextInput {
				log.Printf("Weird, node with input 'text' is not string, but %v", nodeInput.Type)
				continue
			}
			return InputRef{nodeId: k, inputId: "text", inputType: ComfyTextInput}, nil
		}
	}

	// Look for inputs "positive" and walk back to "text"
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["positive"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundText(workflow, k, []string{"value", "text", "positive", "prompt", "conditioning"},
			[]string{"ConditioningZeroOut"})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find positive prompt in the workflow")
}

func FindNegativePrompt(workflow ComfyWorkflow) (InputRef, error) {

	// Fuzzy search for node that has "Positive Prompt" in title
	for k, node := range workflow.Nodes {
		if strings.Contains(node.Title, "Negative Prompt") {
			nodeInput, found := node.Inputs["text"]
			if !found {
				continue
			}
			if nodeInput.Type != ComfyTextInput {
				log.Printf("Weird, node with input 'text' is not string, but %v", nodeInput.Type)
				continue
			}
			return InputRef{nodeId: k, inputId: "text", inputType: ComfyTextInput}, nil
		}
	}

	// Look for inputs "positive" and walk back to "text"
	for k, node := range workflow.Nodes {
		_, found := node.Inputs["negative"]
		if !found {
			continue
		}
		ref, found := crawlUntilFoundText(workflow, k, []string{"value", "text", "negative", "prompt", "conditioning"},
			[]string{"ConditioningZeroOut"})
		if found {
			return ref, nil
		}
	}
	return InputRef{}, errors.New("Unable to find negative prompt in the workflow")
}

func FindImage(workflow ComfyWorkflow) (InputRef, error) {
	for k, node := range workflow.Nodes {
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

func crawlUntilFoundText(workflow ComfyWorkflow, startNode string, followInputs []string, bannedClasses []string) (InputRef, bool) {
	ref, found := crawlUntilFound(workflow, startNode, ComfyTextInput, followInputs, bannedClasses)
	if !found {
		return InputRef{}, found
	}
	return ref, found
}

func crawlUntilFoundNumber(workflow ComfyWorkflow, startNode string, followInputs []string, bannedClasses []string) (InputRef, bool) {
	ref, found := crawlUntilFound(workflow, startNode, ComfyNumberInput, followInputs, bannedClasses)
	if !found {
		return InputRef{}, found
	}
	return ref, found
}

func crawlUntilFoundBool(workflow ComfyWorkflow, startNode string, followInputs []string, bannedClasses []string) (InputRef, bool) {
	ref, found := crawlUntilFound(workflow, startNode, ComfyBoolInput, followInputs, bannedClasses)
	if !found {
		return InputRef{}, found
	}
	return ref, found
}
func crawlUntilFound(workflow ComfyWorkflow, startNode string, targetType ComfyNodeInputType, followInputs []string, bannedClasses []string) (InputRef, bool) {
	invalidRes := InputRef{}
	currentNode, found := workflow.Nodes[startNode]
	if !found {
		return invalidRes, false
	}
	if slices.ContainsFunc(bannedClasses, func(class string) bool { return class == currentNode.ClassType }) {
		// dont follow nodes of 'banned classes'
		return invalidRes, false
	}
	for _, inputKey := range followInputs {
		inputEntry, found := currentNode.Inputs[inputKey]
		if found {
			if inputEntry.Type == targetType {
				// finish crawl, found the right type
				return InputRef{nodeId: startNode, inputId: inputKey, inputType: targetType}, true
			}
			if inputEntry.Type == ComfyNodeRef {
				ref, found := crawlUntilFound(workflow, inputEntry.OutputPtr.NodeRef, targetType, followInputs,
					bannedClasses)
				if found {
					return ref, true
				}
			}
		}
	}
	return invalidRes, false
}
