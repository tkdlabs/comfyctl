package main

import (
	"bufio"
	"errors"
	"fmt"
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
  seed:		int: seed, or "random" to use random number`

func cmdSet(args []string) error {
	if len(args) != 2 {
		return errors.New(setUsage)
	}
	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error opening %s: %v\n", args[0], err)
	}
	var inputRef InputRef
	switch args[0] {
	case "positive":
		inputRef, err = FindPositivePrompt(cw)
	case "negative":
		inputRef, err = FindNegativePrompt(cw)
	case "width":
		inputRef, err = FindWidth(cw)
	case "height":
		inputRef, err = FindHeight(cw)
	case "fps":
		inputRef, err = FindFps(cw)
	case "batch":
		inputRef, err = FindBatchSize(cw)
	case "seed":
		inputRef, err = FindSeed(cw)
	case "image":
		inputRef, err = FindImage(cw)
	default:
		return fmt.Errorf("Unknown property: %s\n\n%s", args[0], setUsage)
	}
	if err != nil {
		return fmt.Errorf("Unable to find '%s'. Check the dump command first.", args[0])
	}
	var valueStr string = args[1]
	if args[0] == "width" || args[0] == "height" || args[0] == "fps" || 
		args[0] == "batch" || args[0] == "seed" {
			valueInt64, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("For %s property expected value to be int. But got: %s",
	args[0], args[1])
			}
			cw.SetInt(inputRef, valueInt64)
	} else {
		cw.SetString(inputRef, valueStr)
	}
	err = cw.WriteOut(os.Stdout)
	if err != nil {
		return fmt.Errorf("I/O error writing out json workflow: %v", err)
	}
	return nil
}
