package cmd

import "bytes"

// newBuf is a tiny helper so the test files don't all import bytes
// individually for the cobra writer plumbing.
func newBuf() *bytes.Buffer { return &bytes.Buffer{} }
