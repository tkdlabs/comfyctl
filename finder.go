package main

import (
	"errors"
	"log"
	"maps"
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
   for _, node := range workflow.Nodes {
	   nodeInput, found := node.Inputs["positive"]
	   if !found {
		   continue
	   }
	   if nodeInput.Type != ComfyNodeRef {
		   log.Printf("Weird, 'positive' input is not a node ref, but %v", nodeInput.Type)
		   continue
	   }
	   targetNode, found := workflow.Nodes[nodeInput.OutputPtr.NodeRef]
	   if !found {
		   log.Printf("Weird, 'positive' input node ref doesn't exist!")
		   continue
	   }
	   for _, found := targetNode.Inputs["text"]; !found; {
		   tn, found := targetNode.Inputs["positive"]
		   if found {
			   if tn.Type == ComfyNodeRef {
				   ttn, found := workflow.Nodes[tn.OutputPtr.NodeRef]
				   if found {
					   targetNode = ttn
					   continue
				   }
			   }
		   }
		   tn, found = targetNode.Inputs["prompt"]
		   if found {
			   if tn.Type == ComfyNodeRef {
				   ttn, found := workflow.Nodes[tn.OutputPtr.NodeRef]
				   if found {
					   targetNode = ttn
					   continue
				   }
			   }
		   }
		   break
	   }
	   targetNodeText, found := targetNode.Inputs["text"]
	   if !found {
		   all_attributes := slices.Collect(maps.Keys(targetNode.Inputs))
		   log.Printf("Weird, 'positive' target input node doesn't have text attribute, instead it has: %v", all_attributes)
		   continue
	   }
	   if targetNodeText.Type != ComfyTextInput {
		   log.Printf("Weird, node with input 'text' is not string, but %v", targetNodeText.Type)
		   continue
	   }
	   return targetNodeText.Text, nil
   }
   return "", errors.New("Unable to find positive prompt in the workflow")
}
