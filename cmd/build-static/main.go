package main

import (
	"os"

	"ffreis-website-compiler/internal/buildcmd"
	"ffreis-website-compiler/internal/logx"
)

func main() {
	logger := logx.New("website-compiler")
	if err := buildcmd.Run(os.Args[1:], logger); err != nil {
		logger.Error("build-static failed", "error", err)
		os.Exit(1)
	}
}
