package main

import (
	"github.com/pavlov061356/entrolint/internal/cli"

	// Blank-import detectors so their init() side-effect registers
	// them in scaling.Registry before the CLI runs.
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/shotgun"
)

func main() {
	cli.Execute()
}
