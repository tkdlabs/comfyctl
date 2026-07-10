package main

import (
	"errors"
	"log"
	"slices"
	"strings"
)

func FindPositivePrompt(workflow ComfyWorkflow) (string, error) {

   // Fuzzy search for node that has "Positive Prompt" in title
   for _, node := range workflow.Nodes {
	if strings.Contains(node.Title, "Positive Prompt") {
		nodeInput, found := node.Inputs["text"]
		if !found {
			continue
		}
		if nodeInput.Type != ComfyTextInput {
			log.Printf("Weird, node with input 'text' is not string, but %v", nodeInput.Type)
			continue
		}
		return nodeInput.Text, nil
	}
   }

   // Look for inputs "positive" and walk back to "text"
   for k, node := range workflow.Nodes {
	   _, found := node.Inputs["positive"]
	   if !found {
		   continue
	   }
	   prompt, found := crawlUntilFoundText(workflow, k, []string{"value", "text", "positive", "prompt", "conditioning"},
   			[]string{"ConditioningZeroOut"})
	   if found {
		   return prompt, nil
	   }
   }
   return "", errors.New("Unable to find positive prompt in the workflow")
}

func FindNegativePrompt(workflow ComfyWorkflow) (string, error) {

   // Fuzzy search for node that has "Positive Prompt" in title
   for _, node := range workflow.Nodes {
	if strings.Contains(node.Title, "Negative Prompt") {
		nodeInput, found := node.Inputs["text"]
		if !found {
			continue
		}
		if nodeInput.Type != ComfyTextInput {
			log.Printf("Weird, node with input 'text' is not string, but %v", nodeInput.Type)
			continue
		}
		return nodeInput.Text, nil
	}
   }

   // Look for inputs "positive" and walk back to "text"
   for k, node := range workflow.Nodes {
	   _, found := node.Inputs["negative"]
	   if !found {
		   continue
	   }
	   prompt, found := crawlUntilFoundText(workflow, k, []string{"value", "text", "negative", "prompt", "conditioning"},
					   []string {"ConditioningZeroOut"})
	   if found {
		   return prompt, nil
	   }
   }
   return "", errors.New("Unable to find negative prompt in the workflow")
}
func crawlUntilFoundText(workflow ComfyWorkflow, startNode string, followInputs []string, bannedClasses []string) (string, bool) {
	currentNode, found := workflow.Nodes[startNode]
	if !found {
		return "", false
	}
	if slices.ContainsFunc(bannedClasses, func (class string) bool { return class == currentNode.ClassType  }) {
		// dont follow nodes of 'banned classes'
		return "", false
	}
	for _, inputKey := range followInputs {
		inputEntry, found := currentNode.Inputs[inputKey]
		if found {
			if inputEntry.Type == ComfyTextInput {
				// finish crawl, found text
				return inputEntry.Text, true
			}
			if inputEntry.Type == ComfyNodeRef {
				recursiveRes, found := crawlUntilFoundText(workflow, inputEntry.OutputPtr.NodeRef, followInputs,
										bannedClasses)
				if found {
					return recursiveRes, true
				}
			}
		}
	}
	return "", false
}
