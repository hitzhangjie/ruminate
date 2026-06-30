package main

import (
	"os"

	"github.com/hitzhangjie/ruminate/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
