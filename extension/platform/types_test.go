// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform_test

import (
	"errors"
	"testing"

	"github.com/larksuite/cli/extension/platform"
)

func TestRiskRank_orderedTaxonomy(t *testing.T) {
	cases := []struct {
		level platform.Risk
		want  int
	}{
		{platform.RiskRead, 0},
		{platform.RiskWrite, 1},
		{platform.RiskHighRiskWrite, 2},
	}
	for _, c := range cases {
		got, ok := platform.RiskRank(c.level)
		if !ok || got != c.want {
			t.Errorf("RiskRank(%q) = (%d,%v), want (%d,true)", c.level, got, ok, c.want)
		}
	}

	if _, ok := platform.RiskRank("unknown-level"); ok {
		t.Fatalf("RiskRank('unknown-level') ok should be false")
	}
	if _, ok := platform.RiskRank(""); ok {
		t.Fatalf("RiskRank('') ok should be false (signals 'no risk annotation')")
	}
}

// The Risk ordering must be strict: read < write < high-risk-write. The
// pruning engine compares ranks; a regression that swaps the order would
// silently let high-risk commands pass under MaxRisk=write.
func TestRiskRank_strictlyMonotonic(t *testing.T) {
	r1, _ := platform.RiskRank(platform.RiskRead)
	r2, _ := platform.RiskRank(platform.RiskWrite)
	r3, _ := platform.RiskRank(platform.RiskHighRiskWrite)
	if !(r1 < r2 && r2 < r3) {
		t.Fatalf("Risk ranks not monotonic: read=%d write=%d high=%d", r1, r2, r3)
	}
}

func TestCommandDeniedError_messageFormats(t *testing.T) {
	withReason := &platform.CommandDeniedError{
		Path:       "docs/+update",
		Layer:      "pruning",
		ReasonCode: "write_not_allowed",
		Reason:     "write disabled by policy",
	}
	if got := withReason.Error(); got != `command "docs/+update" denied: write disabled by policy` {
		t.Fatalf("Error() with Reason = %q", got)
	}

	noReason := &platform.CommandDeniedError{
		Path:       "docs/+update",
		Layer:      "strict_mode",
		ReasonCode: "identity_not_supported",
	}
	if got := noReason.Error(); got != `command "docs/+update" denied (strict_mode/identity_not_supported)` {
		t.Fatalf("Error() without Reason = %q", got)
	}
}

// errors.As must work so consumers can type-assert without unwrap gymnastics.
func TestCommandDeniedError_satisfiesErrorsAs(t *testing.T) {
	var err error = &platform.CommandDeniedError{Path: "x"}
	var target *platform.CommandDeniedError
	if !errors.As(err, &target) {
		t.Fatalf("errors.As should match CommandDeniedError")
	}
	if target.Path != "x" {
		t.Fatalf("target.Path = %q, want %q", target.Path, "x")
	}
}
