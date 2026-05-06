package main

import (
	"fmt"
	"os"

	"github.com/szhekpisov/helm-diffyml/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Cobra has already printed the user-facing error; this is a
		// safety net for unexpected paths.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
