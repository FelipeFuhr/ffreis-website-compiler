package main

import (
	"fmt"
	"os"

	"ffreis-website-compiler/internal/buildcmd"
	"ffreis-website-compiler/internal/exportsitedatacmd"
	"ffreis-website-compiler/internal/logx"
	"ffreis-website-compiler/internal/servecmd"
	"ffreis-website-compiler/internal/validateassetscmd"
	"ffreis-website-compiler/internal/validatedatacmd"
	"ffreis-website-compiler/internal/validatesanitycmd"
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
	case "validate-site-data":
		err = validatedatacmd.Run(args, logger)
	case "validate-assets":
		err = validateassetscmd.Run(args, logger)
	case "export-site-data":
		err = exportsitedatacmd.Run(args, logger)
	case "validate-sanity":
		err = validatesanitycmd.Run(args, logger)
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
	fmt.Print(`website-compiler (legacy alias command)

Usage:
  website-compiler <command> [flags]

Commands:
  build, compile   Build static website output
  serve, web       Start local website server
  export-site-data  Export merged site data (incl. layers) as JSON/YAML
  validate-site-data  Validate site data against the required local site contract
  validate-assets     Validate local CSS/JS assets are reachable from rendered pages
  validate-sanity     Run a baseline set of sanity checks (site data contract + invariants + optional asset reachability)

Examples:
  website-compiler build -out dist
  website-compiler serve -addr :8080
  website-compiler export-site-data -website-root ../my-website -format json
  website-compiler validate-site-data -website-root ../my-website
  website-compiler validate-assets -website-root ../my-website
  website-compiler validate-sanity -website-root ../my-website
`)
}
