package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type ComfyFormat int

const (
	Unknown  ComfyFormat = iota
	API
	GUI // GUI is unsuitable to seding over /prompt endpoint
)

type ComfyWorkflow struct {
	Raw   map[string]any
	Nodes map[string]ComfyNode
}

func OpenComfyWorkflow(reader io.Reader) (ComfyWorkflow, error) {
	var result ComfyWorkflow
	var err error

	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&result.Raw); err != nil {
		return result, err
	}

	switch CheckFormat(result.Raw) {
	case GUI:
		return result, errors.New("Detected GUI format, this is not gonna work for API")
	case Unknown:
		return result, errors.New("Unknown format. Either new ComfyUI format that is not supported or corrupted file.")
	}
	result.Nodes, err = ParseNodesMap(result.Raw)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c ComfyWorkflow) Resolve(inputRef InputRef) (any, error) {
	node, found := c.Nodes[inputRef.nodeId]
	if !found {
		return nil, errors.New(fmt.Sprintf("Invalid InputRef, %s node not found in workflow.", inputRef.nodeId))
	}
	input, found := node.Inputs[inputRef.inputId]
	if !found {
		return nil, errors.New(fmt.Sprintf("Invalid InputRef, %s node does not have %s input.", inputRef.nodeId, inputRef.inputId))
	}
	if input.Type != inputRef.inputType {
		return nil, errors.New(fmt.Sprintf("Invalid InputRef, %s->%s input type mismatch: %v (expected) vs %v.", 
							inputRef.nodeId, inputRef.inputId, inputRef.inputType, input.Type))
	}
	switch input.Type {
	case ComfyNumberInput:
		return input.Number, nil
	case ComfyTextInput:
		return input.Text, nil
	case ComfyBoolInput:
		return input.Bool, nil
	case ComfyNodeRef:
		return fmt.Sprintf("[Node: %s, Output %d]", input.OutputPtr.NodeRef, input.OutputPtr.OutputIdx), nil
	}
	return nil, errors.New("Unknown node type!")
}

func (cw *ComfyWorkflow) SetString(inputRef InputRef, value string) error {
	node, found := cw.Nodes[inputRef.nodeId]
	if !found {
		return errors.New(fmt.Sprintf("Invalid InputRef, %s node not found in workflow.", inputRef.nodeId))
	}
	input, found := node.Inputs[inputRef.inputId]
	if !found {
		return errors.New(fmt.Sprintf("Invalid InputRef, %s node does not have %s input.", inputRef.nodeId, inputRef.inputId))
	}
	input.Type = ComfyTextInput
	input.Text = value
	nodeRaw, found := cw.Raw[inputRef.nodeId]
	if !found {
		return errors.New(fmt.Sprintf("Internal error. Node %s found in structured but not raw maps.", inputRef.nodeId))
	}
	nodeRawMap, ok := nodeRaw.(map[string]any)
	if !ok {
		return errors.New(fmt.Sprintf("Internal error. Node %s is not structured as map in raw format.", inputRef.nodeId))
	}
	inputMapRaw, found := nodeRawMap["inputs"]
	if !found {
		return errors.New(fmt.Sprintf("Internal error. Node %s has no 'inputs' in raw format.", inputRef.nodeId))
	}
	inputMapRawMap, ok := inputMapRaw.(map[string]any)
	if !ok {
		return errors.New(fmt.Sprintf("Internal error. Node %s['inputs'] is not structured as map in raw format.", inputRef.nodeId))
	}
	_, found = inputMapRawMap[inputRef.inputId]
	if !found {
		return errors.New(fmt.Sprintf("Internal error. Node %s has no %s input in raw format.", 
			inputRef.nodeId, inputRef.inputId))
	}
	inputMapRawMap[inputRef.inputId] = value
	return nil
}

func (cw *ComfyWorkflow) SetInt(inputRef InputRef, value int64) error {
	node, found := cw.Nodes[inputRef.nodeId]
	if !found {
		return errors.New(fmt.Sprintf("Invalid InputRef, %s node not found in workflow.", inputRef.nodeId))
	}
	input, found := node.Inputs[inputRef.inputId]
	if !found {
		return errors.New(fmt.Sprintf("Invalid InputRef, %s node does not have %s input.", inputRef.nodeId, inputRef.inputId))
	}
	input.Type = ComfyNumberInput
	input.Number = float64(value)
	nodeRaw, found := cw.Raw[inputRef.nodeId]
	if !found {
		return errors.New(fmt.Sprintf("Internal error. Node %s found in structured but not raw maps.", inputRef.nodeId))
	}
	nodeRawMap, ok := nodeRaw.(map[string]any)
	if !ok {
		return errors.New(fmt.Sprintf("Internal error. Node %s is not structured as map in raw format.", inputRef.nodeId))
	}
	inputMapRaw, found := nodeRawMap["inputs"]
	if !found {
		return errors.New(fmt.Sprintf("Internal error. Node %s has no 'inputs' in raw format.", inputRef.nodeId))
	}
	inputMapRawMap, ok := inputMapRaw.(map[string]any)
	if !ok {
		return errors.New(fmt.Sprintf("Internal error. Node %s['inputs'] is not structured as map in raw format.", inputRef.nodeId))
	}
	_, found = inputMapRawMap[inputRef.inputId]
	if !found {
		return errors.New(fmt.Sprintf("Internal error. Node %s has no %s input in raw format.", 
			inputRef.nodeId, inputRef.inputId))
	}
	inputMapRawMap[inputRef.inputId] = float64(value)
	return nil
}

func (cw ComfyWorkflow) WriteOut(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	return encoder.Encode(cw.Raw)
}

// Node 
type ComfyNode struct {
	Inputs map[string]ComfyNodeInput
	ClassType string
	Title  string
}

// one-of
type ComfyNodeInputType int

const (
	UnknownNodeInputType ComfyNodeInputType = iota
	ComfyNumberInput
	ComfyTextInput
	ComfyBoolInput
	ComfyNodeRef
)

type ComfyNodeInput struct {
	Type	ComfyNodeInputType
	Number	float64
	Text	string
	Bool    bool
	OutputPtr ComfyNodeOutput
}

type ComfyNodeOutput struct {
	NodeRef	string
	OutputIdx int
}

type InputRef struct {
	nodeId	string
	inputId	string
	inputType ComfyNodeInputType
}
