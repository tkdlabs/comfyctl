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

const markUsage = `comfyctl mark [-i workflow] [role] [optional ref] - marks specific workflow input with designated role.

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
  -d, --delete role	Delete the marker for the given role everywhere it is marked,
                        returning those nodes to heuristic (fuzzy) resolution.
  -f, --overwrite	Allow replacing a role that is already marked on the target node,
                        or moving a role that is marked on another node.
  [role]                Allows to specify any predefined roles, but also custom roles (any other string)
                        If you persist custom role mapping you'll be able to set it via "set" command
  [optional ref]	If provided, will skip interactive mode, and instead automatically use the input
                        mapping provided. Format "node:input_name", eg. "116:42:value", value input of 
			node "116:42"`

type markOpts struct {
	workflowPath string
	role         string
	ref          InputRef
	overwrite    bool
	delete       bool
}

func parseMarkArgs(args []string) (markOpts, []string, error) {
	var opts markOpts
	var rest []string
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "-i":
			if i+1 >= len(args) {
				return opts, rest, fmt.Errorf("flag -i requires a workflow file argument")
			}
			opts.workflowPath = args[i+1]
			i++
		case "-f", "--overwrite":
			opts.overwrite = true
		case "-d", "--delete":
			opts.delete = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, markUsage)
			return opts, rest, flag.ErrHelp
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func cmdMark(args []string) error {
	opts, rest, err := parseMarkArgs(args)
	if err == flag.ErrHelp {
		return nil // usage already printed
	}
	if err != nil {
		return err
	}
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
	if len(rest) > 2 {
		return fmt.Errorf("Too many arguments: %v\n\n%s", rest, markUsage)
	}
	if opts.delete {
		if len(rest) != 1 {
			return fmt.Errorf("-d/--delete requires exactly one [role] argument\n\n%s", markUsage)
		}
		if opts.ref.nodeId != "" || opts.ref.inputId != "" {
			return fmt.Errorf("-d/--delete does not take a [ref]; it deletes the role wherever it is marked")
		}
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

	if opts.delete {
		if err := runMarkDelete(&cw, opts); err != nil {
			return err
		}
		return nil
	}

	var target InputRef
	if opts.ref.nodeId != "" && opts.ref.inputId != "" {
		target = opts.ref
	} else {
		var mapToRef map[int]InputRef = make(map[int]InputRef)
		for i, input := range FindAllNonRefInputs(cw) {
			val, err := cw.Resolve(input)
			if err != nil {
				return fmt.Errorf("Error resolving %v: %v", input, err)
			}
			mapToRef[i+1] = input
			marker := cw.Nodes[input.nodeId].MarkerRole
			if marker != "" {
				fmt.Fprintf(os.Stderr, "%d: {class:%s} [%s:%s]  %v  (already marked as '%s')\n", i+1, cw.resolveClass(input.nodeId), input.nodeId, input.inputId, val, marker)
			} else {
				fmt.Fprintf(os.Stderr, "%d: {class:%s} [%s:%s]  %v\n", i+1, cw.resolveClass(input.nodeId), input.nodeId, input.inputId, val)
			}
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
				target = mapToRef[int(number)]
				break
			} else {
				fmt.Fprintf(os.Stderr, "Try again, only enter number between %d and %d\n", 1, len(mapToRef))
			}
		}
	}
	if target.nodeId == "" || target.inputId == "" {
		return fmt.Errorf("No input selected; leaving workflow unchanged.")
	}

	// Overwrite protection: a node holds exactly one marker (`_meta.comfyctl`
	// is a single object), and a role should live in exactly one place. Refuse
	// to clobber either unless -f is given; with -f, move the role cleanly.
	existingNode, err := cw.FindRole(opts.role)
	if err != nil {
		return fmt.Errorf("Internal error while finding existing marker of '%s' role: %v", opts.role, err)
	}
	if existingNode != "" && existingNode != target.nodeId {
		if !opts.overwrite {
			return fmt.Errorf("Role '%s' is already marked on node %s. Use -f to move it to %s:%s.",
				opts.role, existingNode, target.nodeId, target.inputId)
		}
		if err := cw.ClearMark(existingNode); err != nil {
			return fmt.Errorf("Error clearing existing marker on node %s: %v", existingNode, err)
		}
		fmt.Fprintf(os.Stderr, "Moved '%s' marker from node %s to %s:%s\n",
			opts.role, existingNode, target.nodeId, target.inputId)
	} else if targetMarker := cw.Nodes[target.nodeId].MarkerRole; targetMarker != "" && targetMarker != opts.role {
		if !opts.overwrite {
			return fmt.Errorf("Node %s already carries the '%s' marker. Use -f to replace it with '%s'.",
				target.nodeId, targetMarker, opts.role)
		}
		fmt.Fprintf(os.Stderr, "Replacing '%s' marker on node %s with '%s'\n",
			targetMarker, target.nodeId, opts.role)
	}

	fmt.Fprintf(os.Stderr, "Applying %s role to %s:%s ref\n", opts.role, target.nodeId, target.inputId)
	if err := cw.MarkRole(target, opts.role); err != nil {
		return fmt.Errorf("Error marking role: %v", err)
	}
	return writeWorkflow(&cw, opts.workflowPath)
}

// runMarkDelete implements `mark -d/--delete <role>`: it removes the marker
// from every node carrying that role (returning them to heuristic resolution)
// and writes the workflow out. It never prompts and needs no ref.
func runMarkDelete(cw *ComfyWorkflow, opts markOpts) error {
	removed, err := cw.DeleteRole(opts.role)
	if err != nil {
		return fmt.Errorf("Error deleting marker(s) for role '%s': %v", opts.role, err)
	}
	if removed == 0 {
		fmt.Fprintf(os.Stderr, "Role '%s' was not marked; workflow unchanged.\n", opts.role)
	} else {
		fmt.Fprintf(os.Stderr, "Deleted %d marker(s) for role '%s'\n", removed, opts.role)
	}
	return writeWorkflow(cw, opts.workflowPath)
}

// writeWorkflow writes a workflow to stdout (filter style) or to the given
// workflowPath in place, matching the read source.
func writeWorkflow(cw *ComfyWorkflow, workflowPath string) error {
	var writer io.Writer
	if workflowPath == "" {
		writer = os.Stdout
	} else {
		file, err := os.Create(workflowPath)
		if err != nil {
			return fmt.Errorf("Unable to open workflow file for writing: %s: %v", workflowPath, err)
		}
		defer file.Close()
		writer = file
	}
	if err := cw.WriteOut(writer); err != nil {
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
