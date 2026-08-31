package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

const rolesUsage = `comfyctl roles - lists every role marked via 'mark'

Reads a workflow from stdin and prints each marked role together with the
node(s) that carry it. Roles are schema-less and per-file: the workflow's
_meta.comfyctl markers are the registry, so this command reads them back with
no global config.

Output goes to stdout. Any uniqueness violation (a role marked on multiple
nodes, or a marker pointing at a missing input) is printed as a note so you
can fix it with 'mark -d <role>' and re-mark.

Usage:
  comfyctl roles < workflow.json`

func cmdRoles(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("Unexpected arguments: %v\n\n%s", args, rolesUsage)
	}

	reader := bufio.NewReader(os.Stdin)
	cw, err := OpenComfyWorkflow(reader)
	if err != nil {
		return fmt.Errorf("Error parsing workflow: %v\n", err)
	}

	loc := cw.markerLocations()
	if len(loc) == 0 {
		fmt.Println("No roles are marked in this workflow.")
		return nil
	}

	// Group nodes by role. markerLocations() is sorted by role then node, so
	// accumulating nodes per role here yields a sorted list per role.
	nodesByRole := make(map[string][]string)
	for _, m := range loc {
		nodesByRole[m.role] = append(nodesByRole[m.role], m.nodeId)
	}
	type roleGroup struct {
		role  string
		nodes []string
	}
	groups := make([]roleGroup, 0, len(nodesByRole))
	for role, nodes := range nodesByRole {
		groups = append(groups, roleGroup{role: role, nodes: nodes})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].role < groups[j].role })

	for _, g := range groups {
		fmt.Printf("%s\t(%d node%s)\t%s\n", g.role, len(g.nodes),
			plural(len(g.nodes)), strings.Join(g.nodes, ", "))
	}

	conflicts, err := cw.DetectMarkerConflicts()
	if err != nil {
		fmt.Printf("Note: unable to check marker conflicts: %v\n", err)
	}
	for _, c := range conflicts {
		if len(c.Nodes) > 1 {
			fmt.Printf("Note: role '%s' is marked on %d nodes (%s); use 'mark -d %s' then re-mark, or 'mark -f' to move.\n",
				c.Role, len(c.Nodes), strings.Join(c.Nodes, ", "), c.Role)
		}
		if c.Dangling != "" {
			fmt.Printf("Note: marker for role '%s' on node %s points at a missing input; use 'mark -d %s' to clear it.\n",
				c.Role, c.Dangling, c.Role)
		}
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
