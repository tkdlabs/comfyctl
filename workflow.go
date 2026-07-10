package main

import (
	"encoding/json"
	"errors"
	"os"
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

func OpenComfyWorkflow(path string) (ComfyWorkflow, error) {
	var result ComfyWorkflow
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
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

