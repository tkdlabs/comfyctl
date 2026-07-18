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

The following <what> attributes are supported:
  positive:	string: positive prompt
  negative:	string: negative promt
  width:	int: width
  height:	int: height
  fps:		int: fps
  image:	string: image path
  batch:	int: batch size
  seed:		int: seed, or "random" to use random number. 
                     If multiple seeds found applies value to all.`

func cmdSet(args []string) error {
	if len(args) != 2 {
		return errors.New(setUsage)
	}
	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error opening %s: %v\n", args[0], err)
	}
	refs, err := cw.ResolveRole(args[0])
	if err != nil {
		return fmt.Errorf("Unable to find '%s'. Check the dump command first.", args[0])
	}
	var valueStr string = args[1]
	for _, ref := range refs {
		if isIntRole(args[0]) {
			if valueStr == "random" {
				cw.SetInt(ref, rand.Int63())
				continue
			}
			valueInt64, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("For %s property expected value to be int. But got: %s",
					args[0], args[1])
			}
			cw.SetInt(ref, valueInt64)
		} else {
			cw.SetString(ref, valueStr)
		}
	}
	err = cw.WriteOut(os.Stdout)
	if err != nil {
		return fmt.Errorf("I/O error writing out json workflow: %v", err)
	}
	return nil
}

func isIntRole(role string) bool {
	return role == "width" || role == "height" || role == "fps" || role == "batch" || role == "seed"
}
