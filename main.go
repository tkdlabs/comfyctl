package main

import (
	"fmt"
	"os"
)

const usage = `comfyctl - tool for viewing/modifying/submitting ComfyUI workflows

Usage:
  comfyctl <command> [flags]

Commands:
  dump		dumps details about the workflow (prompts, image sources, resolution, seed)
  set		changes details about the workflow
  submit	submits the workflow to ComfyUI`

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
	case "set":
		err = cmdSet(cmdArgs)
	case "submit":
		err = cmdSubmit(cmdArgs)
	case "-h", "--help", "help":
		fmt.Println(usage)
		return
	default:
		fmt.Printf("unkown command %q\n\n%s\n", cmd, usage)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
