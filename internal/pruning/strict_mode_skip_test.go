// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pruning_test

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/policydecision"
	"github.com/larksuite/cli/internal/pruning"
)

// pruning.Apply MUST NOT overwrite the denial annotation on a command
// already marked as strict-mode denied. strict-mode is a hard boundary
// (credential-derived); a user-layer rule cannot relabel or replace
// the error path.
//
// Without this invariant: when a user yaml rule happened to match the
// path of a strict-mode stub, Apply would change layer=strict_mode to
// layer=pruning, and the user-visible error would say "denied by yaml"
// instead of "strict mode". The hard-boundary contract demands
// strict_mode wins.
func TestApply_PreservesStrictModeAnnotation(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	stub := &cobra.Command{
		Use:    "victim",
		Hidden: true,
		Annotations: map[string]string{
			pruning.AnnotationDenialLayer:  policydecision.LayerStrictMode,
			pruning.AnnotationDenialSource: "strict-mode",
		},
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	root.AddCommand(stub)

	// User-layer pruning denies the same path.
	denied := map[string]policydecision.Denial{
		"victim": {
			Layer:        policydecision.LayerPruning,
			PolicySource: "yaml",
			Reason:       "denied by user yaml",
			ReasonCode:   "command_denylisted",
		},
	}
	pruning.Apply(root, denied)

	if got := stub.Annotations[pruning.AnnotationDenialLayer]; got != policydecision.LayerStrictMode {
		t.Errorf("strict-mode layer overwritten by pruning: got %q want %q",
			got, policydecision.LayerStrictMode)
	}
	if got := stub.Annotations[pruning.AnnotationDenialSource]; got != "strict-mode" {
		t.Errorf("strict-mode source overwritten: got %q", got)
	}
}

// Sanity: a normal command (no prior annotation) still gets the
// pruning denial annotations after Apply.
func TestApply_NonStrictCommandStillGetsPruningAnnotation(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	leaf := &cobra.Command{
		Use:  "normal",
		RunE: func(*cobra.Command, []string) error { return nil },
	}
	root.AddCommand(leaf)

	denied := map[string]policydecision.Denial{
		"normal": {
			Layer:        policydecision.LayerPruning,
			PolicySource: "yaml",
			Reason:       "denied",
			ReasonCode:   "command_denylisted",
		},
	}
	pruning.Apply(root, denied)

	if got := leaf.Annotations[pruning.AnnotationDenialLayer]; got != policydecision.LayerPruning {
		t.Errorf("expected pruning layer annotation, got %q", got)
	}
}
