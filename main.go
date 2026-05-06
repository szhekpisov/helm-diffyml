package main

import (
	"fmt"
	"os"

	"github.com/szhekpisov/helm-diffyml/cmd"
)

// osExit is a package-level indirection so the unit test can stub it; calling
// os.Exit directly from main would terminate the test binary.
var osExit = os.Exit

func main() {
	osExit(run())
}

// run returns the exit code so the logic is testable in isolation.
func run() int {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}
