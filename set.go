package main

import (
	"errors"
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
}
