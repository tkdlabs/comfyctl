package main

import (
	"encoding/json"
	"fmt"
)


func CheckFormat(workflow map[string]any) ComfyFormat {
	_, found := workflow["nodes"]
	if found {
		return GUI
	}
	_, found = workflow["last_node_id"]
	if found {
		return GUI
	}
	for k := range workflow {
		_, err := extractMap(workflow, k)
		if err != nil {
			return Unknown
		}
	}
	return API
}
func ParseNodesMap(workflow_map map[string]any) (map[string]ComfyNode, error) {
	var result = make(map[string]ComfyNode)

	for k := range workflow_map {
		nodeMap, err := extractMap(workflow_map, k)
		if err != nil {
			return result, err
		}
		comfyNode, err := parseComfyNode(nodeMap)
		if err != nil {
			return result, err
		}
		result[k] = comfyNode
	}

	return result, nil
}

func parseComfyNode(node_map map[string]any) (ComfyNode, error) {
	var result = ComfyNode { }

	for k := range node_map {
		switch k {
		case "inputs":
			inputMap, err := extractMap(node_map, k)
			if err != nil {
				return result, err
			}
			result.Inputs, err = parseComfyInputs(inputMap)
			if err != nil {
				return result, err
			}
		case "class_type":
			classType, err := extractString(node_map, k)
			if err != nil {
				return result, err
			}
			result.ClassType = classType
		case "_meta":
			metaMap, err := extractMap(node_map, k)
			if err != nil {
				return result, err
			}
			result.Title, err = parseMetaTitle(metaMap)
			if err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func parseComfyInputs(inputMap map[string]any) (map[string]ComfyNodeInput, error) {
	var result = make(map[string]ComfyNodeInput)

	for k, v := range inputMap {
		// try extract different types
		switch v := v.(type) {
		case json.Number:
			result[k] = ComfyNodeInput { Type: ComfyNumberInput, Number: v }
		case string:
			result[k] = ComfyNodeInput { Type: ComfyTextInput, Text: v }
		case bool:
			result[k] = ComfyNodeInput { Type: ComfyBoolInput, Bool: v }
		case []any:
			nodeRef, idx, err := parseNodeRef(v)
			if err != nil {
				return result, err
			}
			result[k] = ComfyNodeInput { Type: ComfyNodeRef, 
			                             OutputPtr: ComfyNodeOutput { 
							     NodeRef: nodeRef, OutputIdx: idx } }
		default:
			return result, fmt.Errorf("Unknown node input type: %s -> %T", k, v)
		}
	}
	return result, nil
}

func parseNodeRef(nodeRef []any) (string, int64, error) {
	var nodeRefTxt = ""
	var outputIdx int64 = 0
	if (len(nodeRef) != 2) {
		return nodeRefTxt, outputIdx, fmt.Errorf("Unexpected nodeRef length, expected:2 got %d: %v",
	len(nodeRef), nodeRef)
	}
	switch nodeRef[0].(type) {
	case string:
		nodeRefTxt = nodeRef[0].(string)
	default:
		return nodeRefTxt, outputIdx, fmt.Errorf("Unexpected nodeRef format nodeRef[0] expected string got %T: %v",
	nodeRef[0], nodeRef[0])
	}
	switch nodeRef[1].(type) {
	case json.Number:
		outputIdx, _ = nodeRef[1].(json.Number).Int64()
	default:
		return nodeRefTxt, outputIdx, fmt.Errorf("Unexpected nodeRef format nodeRef[1] expected number got %T: %v",
	nodeRef[1], nodeRef[1])
	}
	return nodeRefTxt, outputIdx, nil
}

func parseMetaTitle(metaMap map[string]any) (string, error) {
	var result = ""
	var found = false

	for k := range metaMap {
		if k == "title" {
			titleVal, err := extractString(metaMap, k)
			if err != nil {
				return result, err
			}
			result = titleVal
			found = true
		}
	}
	if found {
		return result, nil
	} else {
		return result, fmt.Errorf("title not found in meta map: %v", metaMap)
	}
}
