package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

type ComfyFormat int

const (
	Unknown ComfyFormat = iota
	API
	GUI // GUI is unsuitable to seding over /prompt endpoint
)

type ComfyWorkflow struct {
	Raw         map[string]any
	Nodes       map[string]ComfyNode
	NodesSynced bool
}

func OpenComfyWorkflow(reader io.Reader) (ComfyWorkflow, error) {
	var result ComfyWorkflow
	var err error

	decoder := json.NewDecoder(reader)
	// Avoid losing precision especially for > 2^53 seeds.
	decoder.UseNumber()
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
	result.NodesSynced = true
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c ComfyWorkflow) ResolveRole(role string) ([]InputRef, error) {
	if refs := c.markedRefs(role); len(refs) > 0 {
		return refs, nil // user-marked refs
	}
	return findByRole(c, role)
}

func (c ComfyWorkflow) markedRefs(role string) []InputRef {
	var refs []InputRef
	for id, node := range c.Nodes {
		if node.MarkerRole != role {
			continue
		}
		t := node.Inputs[node.MarkerInput].Type
		refs = append(refs, InputRef{nodeId: id, inputId: node.MarkerInput, inputType: t})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].nodeId < refs[j].nodeId })
	return refs
}

func (c ComfyWorkflow) Resolve(inputRef InputRef) (any, error) {
	if !c.NodesSynced {
		return nil, fmt.Errorf("The parsed nodes are not synced to current version.")
	}
	node, found := c.Nodes[inputRef.nodeId]
	if !found {
		return nil, fmt.Errorf("Invalid InputRef, %s node not found in workflow.", inputRef.nodeId)
	}
	input, found := node.Inputs[inputRef.inputId]
	if !found {
		return nil, fmt.Errorf("Invalid InputRef, %s node does not have %s input.", inputRef.nodeId, inputRef.inputId)
	}
	if input.Type != inputRef.inputType {
		return nil, fmt.Errorf("Invalid InputRef, %s->%s input type mismatch: %v (expected) vs %v.",
			inputRef.nodeId, inputRef.inputId, inputRef.inputType, input.Type)
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
	inputMap, err := cw.getRawInputMap(inputRef)
	if err != nil {
		return err
	}
	inputMap[inputRef.inputId] = value
	cw.NodesSynced = false
	return nil
}

func (cw *ComfyWorkflow) SetInt(inputRef InputRef, value int64) error {
	inputMap, err := cw.getRawInputMap(inputRef)
	if err != nil {
		return err
	}
	inputMap[inputRef.inputId] = json.Number(strconv.FormatInt(value, 10))
	cw.NodesSynced = false
	return nil
}

func (cw ComfyWorkflow) getRawInputMap(inputRef InputRef) (map[string]any, error) {
	nodeRaw, found := cw.Raw[inputRef.nodeId]
	if !found {
		return nil, fmt.Errorf("Internal error. Node %s found in structured but not raw maps.", inputRef.nodeId)
	}
	nodeRawMap, ok := nodeRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Internal error. Node %s is not structured as map in raw format.", inputRef.nodeId)
	}
	inputMapRaw, found := nodeRawMap["inputs"]
	if !found {
		return nil, fmt.Errorf("Internal error. Node %s has no 'inputs' in raw format.", inputRef.nodeId)
	}
	inputMapRawMap, ok := inputMapRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Internal error. Node %s['inputs'] is not structured as map in raw format.", inputRef.nodeId)
	}
	_, found = inputMapRawMap[inputRef.inputId]
	if !found {
		return nil, fmt.Errorf("Internal error. Node %s has no %s input in raw format.",
			inputRef.nodeId, inputRef.inputId)
	}
	return inputMapRawMap, nil
}

func (cw ComfyWorkflow) WriteOut(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	return encoder.Encode(cw.Raw)
}

// Node
type ComfyNode struct {
	Inputs      map[string]ComfyNodeInput
	ClassType   string
	Title       string
	MarkerRole  string
	MarkerInput string
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
	Type      ComfyNodeInputType
	Number    json.Number
	Text      string
	Bool      bool
	OutputPtr ComfyNodeOutput
}

type ComfyNodeOutput struct {
	NodeRef   string
	OutputIdx int64
}

type InputRef struct {
	nodeId    string
	inputId   string
	inputType ComfyNodeInputType
}
