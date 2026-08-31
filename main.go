package main

import (
	"fmt"
	"os"
)

const usage = `comfyctl - tool for viewing/modifying/submitting ComfyUI workflows

Usage:
  comfyctl <command> [flags]
  comfyctl --version

Commands:
  dump		dumps details about the workflow (prompts, image sources, resolution, seed)
  mark		manually maps a workflow input to a role (persisted in _meta.comfyctl);
			mark -d <role> deletes a marker
  roles		lists every marked role and the nodes carrying it
  set		changes details about the workflow
  submit	submits the workflow to ComfyUI
  version	prints comfyctl version/build metadata`

// Build metadata injected at release time via goreleaser -ldflags (-X).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage)
		os.Exit(2)
	}

	cmd := os.Args[1]
	cmdArgs := os.Args[2:]
	var err error

	switch cmd {
	case "dump":
		err = cmdDump(cmdArgs)
	case "mark":
		err = cmdMark(cmdArgs)
	case "roles":
		err = cmdRoles(cmdArgs)
	case "set":
		err = cmdSet(cmdArgs)
	case "submit":
		err = cmdSubmit(cmdArgs)
	case "version":
		fmt.Println(versionString())
		return
	case "--version", "-v":
		fmt.Println(versionString())
		return
	case "-h", "--help", "help":
		fmt.Println(usage)
		return
	default:
		fmt.Printf("unknown command %q\n\n%s\n", cmd, usage)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func versionString() string {
	return fmt.Sprintf("comfyctl %s (commit: %s, built: %s)", version, commit, date)
}
