package main

import (
	"github.com/pavlov061356/entrolint/internal/cli"

	// Blank-import detectors so their init() side-effect registers
	// them in scaling.Registry before the CLI runs.
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/identifierfanout"
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/implementorscan"
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/shotgun"
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/statemultiplier"
	_ "github.com/pavlov061356/entrolint/internal/scaling/detectors/switchsymmetry"
)

func main() {
	cli.Execute()
}
