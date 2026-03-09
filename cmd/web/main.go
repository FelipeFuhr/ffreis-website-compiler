package main

import (
	"os"

	"ffreis-website-compiler/internal/logx"
	"ffreis-website-compiler/internal/servecmd"
)

func main() {
	logger := logx.New("website-compiler")
	if err := servecmd.Run(os.Args[1:], logger); err != nil {
		logger.Error("serve command failed", "error", err)
		os.Exit(1)
	}
}
