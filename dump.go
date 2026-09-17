package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// TODO: rework
const dumpUsage = `comfyctl dump [flags] <what> - tries to find workflow crucial data that can be overridden

Note: this uses best-effort search; roles marked with 'mark' take precedence.
If no <what> is provided, tool outputs all attributes after trying.

Flags:
  --json   emit machine-readable JSON on stdout instead of prose. Marker notes
           and resolution failures never pollute the data stream: unresolved
           roles appear as "error" entries inside the object.

The following <What> attributes are supported. You can supply multiple <what>:
  positive:	finds positive prompt
  negative:	finds negative prompt
  width:	output artifact's width
  height:	output artifact's height
  fps:		output artifact's fps (may not work for image workflows)
  image:	input image for I2I or I2V workflows (assumes one input image)
  batch:	batch size set in the workflow
  seed:		seed used to generate artifact
  checkpoint:	model checkpoint used
  steps:	sampling steps
  cfg:		cfg scale
  denoise:	denoise strength`

type roleDescriptor struct {
	RoleText string
}

var PredefinedRoles = map[string]roleDescriptor{
	"positive":   {"positive prompt"},
	"negative":   {"negative prompt"},
	"width":      {"width of output artifact"},
	"height":     {"height of output artifact"},
	"fps":        {"frames per second"},
	"image":      {"input image"},
	"batch":      {"batch size"},
	"seed":       {"seed"},
	"checkpoint": {"model checkpoint"},
	"steps":      {"sampling steps"},
	"cfg":        {"cfg scale"},
	"denoise":    {"denoise strength"},
}

func cmdDump(args []string) error {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, dumpUsage) }
	jsonFlag := fs.Bool("json", false, "emit machine-readable JSON on stdout")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	args = fs.Args()

	display := make(map[string]roleDescriptor)
	for _, arg := range args {
		switch arg {
		case "positive", "negative", "width", "height", "batch", "fps", "image", "seed",
			"checkpoint", "steps", "cfg", "denoise":
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
	if len(args) == 0 {
		display = maps.Clone(PredefinedRoles)
		extra_markers, err := cw.FindAllMarkedRoles()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error while scanning for marked roles, will stick to predefined ones: %v\n", err)
		} else {
			for _, role := range extra_markers {
				display[role] = roleDescriptor{fmt.Sprintf("custom role marker '%s'", role)}
			}
		}
	}

	if *jsonFlag {
		if err := dumpJSON(cw, display); err != nil {
			return err
		}
		printMarkerConflicts(cw)
		return nil
	}
	printMarkerConflicts(cw)

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

// roleDumpEntry is one role's resolution result in dump --json output: either
// the resolved value(s) plus the node inputs they live on, or an error string.
// A single ref yields a scalar Value; multiple refs yield a list.
type roleDumpEntry struct {
	Value any            `json:"value,omitempty"`
	Nodes []dumpLocation `json:"nodes,omitempty"`
	Error string         `json:"error,omitempty"`
}

type dumpLocation struct {
	Node  string `json:"id"`
	Input string `json:"input"`
}

// dumpJSON resolves every requested role and encodes the results as a JSON
// object on stdout. Errors are embedded per role, never written to stdout, so
// the stream is safe to pipe into jq.
func dumpJSON(cw ComfyWorkflow, display map[string]roleDescriptor) error {
	out := make(map[string]roleDumpEntry, len(display))
	for role := range display {
		entry := roleDumpEntry{}
		vals, err := cw.ResolveRole(role)
		if err != nil {
			out[role] = roleDumpEntry{Error: err.Error()}
			continue
		}
		var values []any
		for _, valref := range vals {
			val, err := cw.Resolve(valref)
			if err != nil {
				entry.Error = fmt.Sprintf("%s (%s:%s): %v", display[role].RoleText, valref.nodeId, valref.inputId, err)
				values = nil
				break
			}
			values = append(values, val)
			entry.Nodes = append(entry.Nodes, dumpLocation{Node: valref.nodeId, Input: valref.inputId})
		}
		if len(values) == 1 {
			entry.Value = values[0]
		} else if len(values) > 1 {
			entry.Value = values
		} else if entry.Error == "" {
			entry.Error = "no values resolved"
		}
		out[role] = entry
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "    ")
	return encoder.Encode(out)
}

// printMarkerConflicts reports marker uniqueness violations to stderr (notes,
// not data): a role marked on more than one node, and markers pointing at a
// missing input. Fixing them is an explicit `mark -d <role>` or `mark -f move`.
func printMarkerConflicts(cw ComfyWorkflow) {
	conflicts, err := cw.DetectMarkerConflicts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error while checking marker conflicts: %v\n", err)
		return
	}
	for _, c := range conflicts {
		if len(c.Nodes) > 1 {
			fmt.Fprintf(os.Stderr, "Note: role '%s' is marked on %d nodes (%s); use 'mark -d %s' then re-mark, or 'mark -f' to move.\n",
				c.Role, len(c.Nodes), strings.Join(c.Nodes, ", "), c.Role)
		}
		if c.Dangling != "" {
			fmt.Fprintf(os.Stderr, "Note: marker for role '%s' on node %s points at a missing input; use 'mark -d %s' to clear it.\n",
				c.Role, c.Dangling, c.Role)
		}
	}
}
