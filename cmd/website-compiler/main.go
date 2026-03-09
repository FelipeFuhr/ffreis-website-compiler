package main

import (
	"fmt"
	"os"

	"ffreis-website-compiler/internal/buildcmd"
	"ffreis-website-compiler/internal/logx"
	"ffreis-website-compiler/internal/servecmd"
)

func main() {
	logger := logx.New("website-compiler")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "build", "compile":
		err = buildcmd.Run(args, logger)
	case "serve", "web":
		err = servecmd.Run(args, logger)
	case "help", "-h", "--help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		logger.Error("command failed", "command", cmd, "error", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`website-compiler

Usage:
  website-compiler <command> [flags]

Commands:
  build, compile   Build static website output
  serve, web       Start local website server

Examples:
  website-compiler build -out ffreis-website-compiler/dist
  website-compiler serve -addr :8080
`)
}
