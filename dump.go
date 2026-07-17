package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const dumpUsage = `comfyctl dump <what> - tries to find workflow crucial data that can be overridden

Note: this uses best-effort finding algorithm (TODO: markers)
If no <what> is provided, tool outputs all attributes after trying.

The following <What> attributes are supported. You can supply multiple <what>:
  positive:	finds positive prompt
  negative:	finds negative prompt
  width:	output artifact's width
  height:	output artifact's height
  fps:		output artifact's fps (may not work for image workflows)
  image:	input image for I2I or I2V workflows (assumes one input image)
  batch:	batch size set in the workflow
  seed:		seed used to generate artifact`

func cmdDump(args []string) error {
	var display map[string]struct{} = make(map[string]struct{})
	if len(args) == 0 {
		display["positive"] = struct{} {}
		display["negative"] = struct{} {}
		display["width"] = struct{} {}
		display["height"] = struct{} {}
		display["batch"] = struct{} {}
		display["fps"] = struct{} {}
		display["image"] = struct{} {}
		display["seed"] = struct{} {}
	}
	for _, arg := range args {
		switch arg {
		case "positive","negative","width","height","batch","fps","image", "seed":
			display[arg] = struct{} {}
		default: 
			return errors.New(
				fmt.Sprintf("Unknown artifact requested: %s\n\n%s", arg, dumpUsage))
		}
	}
	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return errors.New(fmt.Sprintf("Error parsing workflow: %v\n", err))
	}
	var requested bool

	_, requested = display["positive"]
	if requested {
		posPrompt, err := FindPositivePrompt(cw)
		if err != nil {
			fmt.Printf("Failed to find negative prompt: %v\n", err)
		} else {
			val, _  := cw.Resolve(posPrompt)
			fmt.Printf("Found positive prompt %s\n", val)
		}
	}

	_, requested = display["negative"]
	if requested {
		negPrompt, err := FindNegativePrompt(cw)
		if err != nil {
			fmt.Printf("Failed to find negative prompt: %v\n", err)
		} else {
			val, _  := cw.Resolve(negPrompt)
			fmt.Printf("Found negative prompt %s\n", val)
		}
	}

	_, requested = display["height"]
	if requested {
		height, err := FindHeight(cw)
		if err != nil {
			fmt.Printf("Failed to find height: %v\n", err)
		} else {
			val, _  := cw.Resolve(height)
			intval, _ := val.(json.Number).Int64()
			fmt.Printf("Found height: %d\n", intval)
		}
	}

	_, requested = display["width"]
	if requested {
		width, err := FindWidth(cw)
		if err != nil {
			fmt.Printf("Failed to find width: %v\n", err)
		} else {
			val, _  := cw.Resolve(width)
			intval, _ := val.(json.Number).Int64()
			fmt.Printf("Found width: %d\n", intval)
		}
	}

	_, requested = display["batch"]
	if requested {
		batch_size, err := FindBatchSize(cw)
		if err != nil {
			fmt.Printf("Failed to find batch size: %v\n", err)
		} else {
			val, _  := cw.Resolve(batch_size)
			intval, _ := val.(json.Number).Int64()
			fmt.Printf("Found batch size: %d\n", intval)
		}
	}

	_, requested = display["seed"]
	if requested {
		seed, err := FindSeed(cw)
		if err != nil {
			fmt.Printf("Failed to find seed: %v\n", err)
		} else {
			val, _  := cw.Resolve(seed)
			intval, _ := val.(json.Number).Int64()
			fmt.Printf("Found seed: %d\n", intval)
		}
	}

	_, requested = display["fps"]
	if requested {
		fps, err := FindFps(cw)
		if err != nil {
			fmt.Printf("Failed to find fps: %v\n", err)
		} else {
			val, _  := cw.Resolve(fps)
			intval, _ := val.(json.Number).Int64()
			fmt.Printf("Found fps: %d\n", intval)
		}
	}

	_, requested = display["image"]
	if requested {
		imagesrc, err := FindImage(cw)
		if err != nil {
			fmt.Printf("Failed to find source image: %v\n", err)
		} else {
			val, _  := cw.Resolve(imagesrc)
			fmt.Printf("Found source image: %s\n", val)
		}
	}

	return nil
}


