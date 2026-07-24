package main

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"slices"
)

// TODO: rework
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

type roleDescriptor struct {
	RoleText string
}

var PredefinedRoles = map[string]roleDescriptor{
	"positive": {"positive prompt"},
	"negative": {"negative prompt"},
	"width":    {"width of output artifact"},
	"height":   {"height of output artifact"},
	"fps":      {"frames per second"},
	"image":    {"input image"},
	"batch":    {"batch size"},
	"seed":     {"seed"},
}

func cmdDump(args []string) error {
	display := make(map[string]roleDescriptor)
	if len(args) == 0 {
		display = maps.Clone(PredefinedRoles)
	}
	for _, arg := range args {
		switch arg {
		case "positive", "negative", "width", "height", "batch", "fps", "image", "seed":
			display[arg] = PredefinedRoles[arg]
		default:
			display[arg] = roleDescriptor{fmt.Sprintf("custom role marker '%s'", arg)}
		}
	}
	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error parsing workflow: %v\n", err)
	}

	sortedDisplayKeys := slices.Sorted(maps.Keys(display))

	for _, k := range sortedDisplayKeys {
		vals, err := cw.ResolveRole(k)
		if err != nil {
			fmt.Printf("Failed to find %s: %v\n", display[k].RoleText, err)
		} else {
			for _, valref := range vals {
				val, err := cw.Resolve(valref)
				if err != nil {
					fmt.Printf("Error resolving %s (%s:%s): %v\n", display[k].RoleText, valref.nodeId, valref.inputId, err)
				} else {
					fmt.Printf("Found %s: %v\n", display[k].RoleText, val)
				}
			}
		}
	}
	return nil
}
