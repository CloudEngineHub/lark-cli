// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package hook

import "io"

// SetStderrForTesting redirects the hook layer's warning output to a
// custom writer. Used by tests to silence stderr or assert on warning
// content without touching os.Stderr.
//
// Production code never calls this; the default writer is os.Stderr via
// defaultStderr.
func SetStderrForTesting(w io.Writer) {
	stderr = func() interface{ Write(p []byte) (int, error) } {
		return w
	}
}
