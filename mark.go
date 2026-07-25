package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const markUsage = `comfyctl mark [-i workfow] [role] [optional ref] - marks specific workflow input with designated role.

This command is usually interactive, and can edit workflow files in-place
as opposed to other commands. It should not be chained typically, although
it allows writing to stdout for ephemeral runs.

The dump/set commands use fuzzy search to find the roles: prompt, seed, batch_size etc
within the workflow. This is not always easy or feasible.
This command allows user to manually associate node input with role.
The role is persisted in _meta map of the node for later reuse.
Once marked, the "set"/"dump" commands will adhere to this mapping vs. doing fuzzy search.

Flags:
  -i workflow.json	The workflow in API format to be marked. If specified this way,
                        will be edited in-line
			If not provided, tool expects workflow on stdin and will output via stdout.
  [role]                Allows to specify any predefined roles, but also custom roles (any other string)
                        If you persist custom role mapping you'll be able to set it via "set" command
  [optional ref]	If provided, will skip interactive mode, and instead automatically use the input
                        mapping provided. Format "node:input_name", eg. "116:42:value", value input of 
			node "116:42"`

type markOpts struct {
	workflowPath string
	role         string
	ref          InputRef
}

func cmdMark(args []string) error {
	var opts markOpts
	fs := flag.NewFlagSet("mark", flag.ContinueOnError)
	fs.StringVar(&opts.workflowPath, "i", "", "Workflow file to be marked in-place")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("[role] is required\n\n%s", markUsage)
	}
	opts.role = rest[0]
	if len(rest) > 1 {
		inpRef, err := ParseRef(rest[1])
		if err != nil {
			return fmt.Errorf("Invalid [ref] format: %v", err)
		}
		opts.ref = inpRef
	}

	var isStdin = false
	var reader io.Reader
	if opts.workflowPath == "" {
		reader = bufio.NewReader(os.Stdin)
		isStdin = true
	} else {
		file, err := os.Open(opts.workflowPath)
		if err != nil {
			return fmt.Errorf("Unable to open workflow file: %s: %v", opts.workflowPath, err)
		}
		defer file.Close()
		reader = file
	}
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error opening workflow file: %v", err)
	}
	existingNode, err := cw.FindRole(opts.role)
	if err != nil {
		return fmt.Errorf("Internal error while finding existing marker of '%s' role: %v", opts.role, err)
	}
	if existingNode != "" {
		fmt.Fprintf(os.Stderr, "Warning, workflow has already marked '%s' role on '%s' node.\n", opts.role, existingNode)
		fmt.Fprintf(os.Stderr, "If this command writes, it will replace that marker\n")
	}

	marked := false
	if opts.ref.nodeId != "" && opts.ref.inputId != "" {
		err := cw.MarkRole(opts.ref, opts.role)
		if err != nil {
			return fmt.Errorf("Error marking role %v: %v", opts.ref, err)
		}
		marked = true
	} else {
		var mapToRef map[int]InputRef = make(map[int]InputRef)
		for i, input := range FindAllNonRefInputs(cw) {
			val, err := cw.Resolve(input)
			if err != nil {
				return fmt.Errorf("Error resolving %v: %v", input, err)
			}
			mapToRef[i+1] = input
			fmt.Fprintf(os.Stderr, "%d: {class:%s} [%s:%s]  %v\n", i+1, cw.resolveClass(input.nodeId), input.nodeId, input.inputId, val)
		}
		if isStdin {
			return fmt.Errorf("You are piping input file to stdin. Interactive mode is not going to work.\n"+
				"Pick the reference [xxx] and use 'mark %s [xxx]' option with that value", opts.role)
		}
		if len(mapToRef) == 0 {
			return fmt.Errorf("No markable inputs found in the workflow")
		}
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Fprintf(os.Stderr, "Enter option %d-%d\n", 1, len(mapToRef))
		for {
			if !scanner.Scan() {
				break
			}
			input := strings.TrimSpace(scanner.Text())
			number, err := strconv.ParseInt(input, 10, 64)
			if err == nil && number >= 1 && number <= int64(len(mapToRef)) {
				n := int(number)
				fmt.Fprintf(os.Stderr, "Applying %s role to %s:%s ref\n", opts.role, mapToRef[n].nodeId,
					mapToRef[n].inputId)
				err := cw.MarkRole(mapToRef[n], opts.role)
				if err != nil {
					return fmt.Errorf("Error marking role: %v", err)
				}
				marked = true
				break
			} else {
				fmt.Fprintf(os.Stderr, "Try again, only enter number between %d and %d\n", 1, len(mapToRef))
			}
		}
	}

	if !marked {
		fmt.Fprintln(os.Stderr, "No role marked; leaving workflow unchanged.")
		return nil
	}

	// Write out
	var writer io.Writer
	if opts.workflowPath == "" {
		writer = os.Stdout
	} else {
		file, err := os.Create(opts.workflowPath)
		if err != nil {
			return fmt.Errorf("Unable to open workflow file for writing: %s: %v", opts.workflowPath, err)
		}
		defer file.Close()
		writer = file
	}
	err = cw.WriteOut(writer)
	if err != nil {
		return fmt.Errorf("I/O error writing out json workflow: %v", err)
	}
	return nil
}

func ParseRef(ref string) (InputRef, error) {
	var res InputRef
	separator := strings.LastIndex(ref, ":")
	if separator == -1 {
		return res, fmt.Errorf("Unable to find ':'. Required format: node:input, but got %v", ref)
	}
	if len(ref) == separator+1 {
		return res, fmt.Errorf("Missing input reference. Required format: node:input, but got %v", ref)
	}
	res.nodeId = ref[0:separator]
	res.inputId = ref[separator+1:]
	return res, nil
}
