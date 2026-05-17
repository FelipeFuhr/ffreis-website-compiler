package main

import (
	"os"

	"ffreis-website-compiler/internal/logx"
	"ffreis-website-compiler/internal/paritycmd"
)

func main() {
	logger := logx.New("check-lang-parity")
	if err := paritycmd.Run(os.Args[1:], logger); err != nil {
		logger.Error("check-lang-parity failed", "error", err)
		os.Exit(1)
	}
}
