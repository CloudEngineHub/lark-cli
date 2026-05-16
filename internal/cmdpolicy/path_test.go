// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmdpolicy_test

import (
	"path/filepath"
	"testing"

	"github.com/larksuite/cli/internal/cmdpolicy"
)

// RedactHomeDir folds two prefixes:
//
//  1. core.GetBaseConfigDir() → "<config>" (covers the
//     LARKSUITE_CLI_CONFIG_DIR override, which is the only way a real
//     deployment writes the policy file outside $HOME).
//  2. The user's home dir → "~" (catches the conventional
//     ~/.lark-cli/policy.yml path when no override is set).
//
// Both folds run in path-prefix space (not string-prefix), so a path
// like "/Usersfoo" never gets folded against "/Users".
func TestRedactHomeDir_foldsConfigDirOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", tmp)

	policyPath := filepath.Join(tmp, "policy.yml")
	got := cmdpolicy.RedactHomeDir(policyPath)
	if got != "<config>/policy.yml" {
		t.Errorf("override path = %q, want <config>/policy.yml", got)
	}

	// A path that equals the config dir itself collapses to "<config>".
	if got := cmdpolicy.RedactHomeDir(tmp); got != "<config>" {
		t.Errorf("exact-prefix path = %q, want <config>", got)
	}
}

// A path outside both the config dir and $HOME stays absolute. This is
// the "no leak introduced" property: redaction never invents a label
// for something it doesn't recognise.
func TestRedactHomeDir_unrelatedPathUnchanged(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", "/var/lib/lark-cli")

	path := "/etc/random/file.yml"
	if got := cmdpolicy.RedactHomeDir(path); got != path {
		t.Errorf("unrelated path = %q, want %q (unchanged)", got, path)
	}
}

// Empty input round-trips. Callers (e.g. `config policy show` with
// no yaml configured) rely on this.
func TestRedactHomeDir_emptyStays(t *testing.T) {
	if got := cmdpolicy.RedactHomeDir(""); got != "" {
		t.Errorf("empty input = %q, want empty string", got)
	}
}
