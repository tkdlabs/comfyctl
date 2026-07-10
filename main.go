package main

import (
	"flag"
	"fmt"
	"os"
)

func main () {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		fmt.Printf("Use: json [workflow.json]. Got %v\n", args)
		os.Exit(1)
	}
	cw, err := OpenComfyWorkflow(args[0])
	if err != nil {
		fmt.Printf("Error opening %s: %v\n", args[0], err)
		os.Exit(1)
	}

	// Find batch size
	
	// Find prompts
	
/*	posPrompt, err := FindPositivePrompt(cw)
	if err != nil {
		fmt.Printf("Failed to find positive prompt: %v\n", err)
	} else {
		fmt.Printf("Found positive prompt %s\n", posPrompt)//: %s\n", posPrompt)
	}*/
	negPrompt, err := FindNegativePrompt(cw)
	if err != nil {
		fmt.Printf("Failed to find negative prompt: %v\n", err)
	} else {
		fmt.Printf("Found negative prompt %s\n", negPrompt)//: %s\n", posPrompt)
	}

	// Find resolution

	// Find FPS
}

