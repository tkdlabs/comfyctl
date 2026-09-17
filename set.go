package main

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
)

const setUsage string = `comfyctl set <what> <val> - sets the workflow attribute

Note: this uses the same mechanism as 'dump' to find attribute location.
The value is written according to the target input's actual type: int/bool/string
inputs get an int/bool/string, and "random" only applies to numeric roles.
Node references are never overwritten.

The following <what> attributes are supported:
  positive:	string: positive prompt
  negative:	string: negative prompt
  width:	int: width
  height:	int: height
  fps:		int: fps
  image:	string: image path
  batch:	int: batch size
  seed:		int: seed, or "random" to use random number. 
                     If multiple seeds found applies value to all.
  checkpoint:	string: model checkpoint used
  steps:	int: sampling steps
  cfg:		number: cfg scale (float where the workflow uses one)
  denoise:	number: denoise strength
Any role marked with 'mark' (including custom ones) works the same way.`

func cmdSet(args []string) error {
	if len(args) != 2 {
		return errors.New(setUsage)
	}
	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error opening workflow: %v", err)
	}
	refs, err := cw.ResolveRole(args[0])
	if err != nil {
		return fmt.Errorf("Unable to find '%s'. Check the dump command first.", args[0])
	}
	var valueStr string = args[1]
	for _, ref := range refs {
		// Type inference: the value shape follows the target input's actual
		// type, not the role name — "seed random" only means something where
		// a number is expected, and ref-typed inputs are never overwritten.
		switch ref.inputType {
		case ComfyNumberInput:
			if valueStr == "random" {
				cw.SetInt(ref, rand.Int63())
				continue
			}
			valueInt64, err := strconv.ParseInt(valueStr, 10, 64)
			if err != nil {
				return fmt.Errorf("Target input %s:%s is numeric; expected int value but got: %s",
					ref.nodeId, ref.inputId, valueStr)
			}
			cw.SetInt(ref, valueInt64)
		case ComfyTextInput:
			cw.SetString(ref, valueStr)
		case ComfyBoolInput:
			valueBool, err := strconv.ParseBool(valueStr)
			if err != nil {
				return fmt.Errorf("Target input %s:%s is boolean; expected true/false but got: %s",
					ref.nodeId, ref.inputId, valueStr)
			}
			cw.SetBool(ref, valueBool)
		case ComfyNodeRef:
			return fmt.Errorf("Input %s:%s is a node reference and will not be overwritten by 'set'; re-mark the role at a scalar input.",
				ref.nodeId, ref.inputId)
		default:
			return fmt.Errorf("Input %s:%s has unknown type; cannot assign value %s.",
				ref.nodeId, ref.inputId, valueStr)
		}
	}
	err = cw.WriteOut(os.Stdout)
	if err != nil {
		return fmt.Errorf("I/O error writing out json workflow: %v", err)
	}
	return nil
}
