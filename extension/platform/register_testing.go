// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

//go:build testing

package platform

// ResetForTesting clears the global plugin registry. Available only
// under `-tags testing`; not part of the public API.
//
// Tests that exercise plugin registration should defer
// `t.Cleanup(platform.ResetForTesting)` so subsequent tests start
// from a clean slate.
func ResetForTesting() { pluginRegistry.reset() }
