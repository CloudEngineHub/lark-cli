// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package policydecision_test

import (
	"testing"

	"github.com/larksuite/cli/internal/policydecision"
)

func TestAggregateChildren_allSameLayerAndReason(t *testing.T) {
	got := policydecision.AggregateChildren([]policydecision.ChildDenial{
		{Path: "docs/+update", Denial: policydecision.Denial{
			Layer: "pruning", PolicySource: "yaml:agent",
			ReasonCode: "write_not_allowed", RuleName: "agent-policy",
		}},
		{Path: "docs/+delete", Denial: policydecision.Denial{
			Layer: "pruning", PolicySource: "yaml:agent",
			ReasonCode: "write_not_allowed", RuleName: "agent-policy",
		}},
	})
	if got.Layer != "pruning" || got.ReasonCode != "write_not_allowed" {
		t.Fatalf("got %+v, want layer=pruning reason=write_not_allowed", got)
	}
	if got.PolicySource != "yaml:agent" || got.RuleName != "agent-policy" {
		t.Fatalf("Source / RuleName should propagate when consistent, got %+v", got)
	}
}

func TestAggregateChildren_sameLayerMixedReasons(t *testing.T) {
	got := policydecision.AggregateChildren([]policydecision.ChildDenial{
		{Denial: policydecision.Denial{Layer: "pruning", ReasonCode: "write_not_allowed"}},
		{Denial: policydecision.Denial{Layer: "pruning", ReasonCode: "domain_not_allowed"}},
	})
	if got.Layer != "pruning" || got.ReasonCode != "mixed_children_pruning" {
		t.Fatalf("got %+v, want layer=pruning reason=mixed_children_pruning", got)
	}
}

func TestAggregateChildren_strictModeBranch(t *testing.T) {
	got := policydecision.AggregateChildren([]policydecision.ChildDenial{
		{Denial: policydecision.Denial{Layer: "strict_mode", ReasonCode: "identity_not_supported"}},
		{Denial: policydecision.Denial{Layer: "strict_mode", ReasonCode: "identity_not_supported"}},
	})
	if got.Layer != "strict_mode" || got.ReasonCode != "identity_not_supported" {
		t.Fatalf("got %+v", got)
	}
	if got.PolicySource != "strict-mode" {
		t.Fatalf("PolicySource = %q, want strict-mode", got.PolicySource)
	}
}

// Mixed layers (some strict_mode, some pruning) collapse to Layer=pruning
// per the tech-doc rule -- a parent group failing for "both" reasons is
// most actionable framed as a user-policy issue (swappable) rather than a
// credential capability one (not swappable).
func TestAggregateChildren_mixedLayersFallsToPruning(t *testing.T) {
	got := policydecision.AggregateChildren([]policydecision.ChildDenial{
		{Path: "docs/+update", Denial: policydecision.Denial{
			Layer: "strict_mode", ReasonCode: "identity_not_supported",
		}},
		{Path: "docs/+fetch", Denial: policydecision.Denial{
			Layer: "pruning", ReasonCode: "domain_not_allowed",
		}},
	})
	if got.Layer != "pruning" {
		t.Fatalf("Layer = %q, want pruning (mixed-children rule)", got.Layer)
	}
	if got.ReasonCode != "all_children_denied" {
		t.Fatalf("ReasonCode = %q, want all_children_denied", got.ReasonCode)
	}
	if got.PolicySource != "mixed" {
		t.Fatalf("PolicySource = %q, want mixed", got.PolicySource)
	}
}

func TestAggregateChildren_emptySlice(t *testing.T) {
	got := policydecision.AggregateChildren(nil)
	if (got != policydecision.Denial{}) {
		t.Fatalf("empty slice should produce zero Denial, got %+v", got)
	}
}

func TestSortChildren_stableOrder(t *testing.T) {
	children := []policydecision.ChildDenial{
		{Path: "docs/+update"},
		{Path: "docs/+delete"},
		{Path: "docs/+create"},
	}
	policydecision.SortChildren(children)
	want := []string{"docs/+create", "docs/+delete", "docs/+update"}
	for i, c := range children {
		if c.Path != want[i] {
			t.Fatalf("children[%d].Path = %q, want %q", i, c.Path, want[i])
		}
	}
}
