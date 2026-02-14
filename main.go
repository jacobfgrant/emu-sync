package main

import (
	"fmt"
	"os"

	"github.com/jacobfgrant/emu-sync/cmd"
)

// version is set by goreleaser via ldflags.
var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
