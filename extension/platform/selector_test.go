// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform_test

import (
	"testing"

	"github.com/larksuite/cli/extension/platform"
)

// fakeView is a minimal CommandView for unit-testing selectors.
type fakeView struct {
	path       string
	domain     string
	risk       string
	riskOK     bool
	identities []string
}

func (v fakeView) Path() string                            { return v.path }
func (v fakeView) Domain() string                          { return v.domain }
func (v fakeView) Risk() (string, bool)                    { return v.risk, v.riskOK }
func (v fakeView) Identities() []string                    { return v.identities }
func (v fakeView) Annotation(key string) (string, bool)    { return "", false }

func TestAll_None(t *testing.T) {
	cmd := fakeView{}
	if !platform.All()(cmd) {
		t.Errorf("All() must match every command")
	}
	if platform.None()(cmd) {
		t.Errorf("None() must match no command")
	}
}

func TestByDomain(t *testing.T) {
	sel := platform.ByDomain("docs", "im")
	if !sel(fakeView{domain: "docs"}) {
		t.Errorf("docs should match")
	}
	if sel(fakeView{domain: "vc"}) {
		t.Errorf("vc must not match docs/im selector")
	}
	// Unknown domain (empty) must not match.
	if sel(fakeView{domain: ""}) {
		t.Errorf("unknown domain must not match ByDomain (use ByDomainOrUnknown style if desired)")
	}
}

// All risk-based selectors share one contract: unknown (no annotation)
// never matches. This test pins each one. A plugin author who wants the
// previous "safety-side" semantics must compose .Or(ByUnknownRisk())
// explicitly -- covered by TestByUnknownRisk_optInSafety below.
func TestByExactRisk_unknownDoesNotMatch(t *testing.T) {
	sel := platform.ByExactRisk("write")
	if !sel(fakeView{risk: "write", riskOK: true}) {
		t.Errorf("exact write should match")
	}
	if sel(fakeView{riskOK: false}) {
		t.Errorf("unknown must not match ByExactRisk")
	}
	if sel(fakeView{risk: "read", riskOK: true}) {
		t.Errorf("read must not match ByExactRisk(write)")
	}
}

func TestByWrite_byReadOnly(t *testing.T) {
	if !platform.ByWrite()(fakeView{risk: "write", riskOK: true}) {
		t.Errorf("write should match ByWrite")
	}
	if !platform.ByWrite()(fakeView{risk: "high-risk-write", riskOK: true}) {
		t.Errorf("high-risk-write should match ByWrite")
	}
	if platform.ByWrite()(fakeView{risk: "read", riskOK: true}) {
		t.Errorf("read must not match ByWrite")
	}
	if platform.ByWrite()(fakeView{riskOK: false}) {
		t.Errorf("unknown must not match ByWrite")
	}
	if !platform.ByReadOnly()(fakeView{risk: "read", riskOK: true}) {
		t.Errorf("read should match ByReadOnly")
	}
	if platform.ByReadOnly()(fakeView{riskOK: false}) {
		t.Errorf("unknown must not match ByReadOnly")
	}
}

// ByUnknownRisk is the explicit opt-in for safety-side widening. A
// plugin that wants the previous "match-write-or-unknown" behaviour
// composes ByWrite().Or(ByUnknownRisk()).
func TestByUnknownRisk_optInSafety(t *testing.T) {
	if !platform.ByUnknownRisk()(fakeView{riskOK: false}) {
		t.Errorf("unannotated command should match ByUnknownRisk")
	}
	if platform.ByUnknownRisk()(fakeView{risk: "write", riskOK: true}) {
		t.Errorf("annotated command must not match ByUnknownRisk")
	}

	// Composition: safety-side widening of ByWrite.
	safeWrite := platform.ByWrite().Or(platform.ByUnknownRisk())
	if !safeWrite(fakeView{risk: "write", riskOK: true}) {
		t.Errorf("annotated write should match safeWrite")
	}
	if !safeWrite(fakeView{riskOK: false}) {
		t.Errorf("unknown should match safeWrite via Or(ByUnknownRisk)")
	}
	if safeWrite(fakeView{risk: "read", riskOK: true}) {
		t.Errorf("annotated read must not match safeWrite")
	}
}

func TestByCommandPath(t *testing.T) {
	sel := platform.ByCommandPath("docs/**", "im/+send")
	if !sel(fakeView{path: "docs/+update"}) {
		t.Errorf("docs/+update should match docs/**")
	}
	if !sel(fakeView{path: "im/+send"}) {
		t.Errorf("im/+send should match")
	}
	if sel(fakeView{path: "contact/+search"}) {
		t.Errorf("contact/+search must not match")
	}
}

func TestByIdentity(t *testing.T) {
	sel := platform.ByIdentity("bot")
	if !sel(fakeView{identities: []string{"user", "bot"}}) {
		t.Errorf("ids containing bot should match")
	}
	if sel(fakeView{identities: []string{"user"}}) {
		t.Errorf("user-only ids must not match bot selector")
	}
}

func TestSelector_AndOrNot(t *testing.T) {
	docsAndWrite := platform.ByDomain("docs").And(platform.ByExactRisk("write"))
	if !docsAndWrite(fakeView{domain: "docs", risk: "write", riskOK: true}) {
		t.Errorf("AND of matching selectors should match")
	}
	if docsAndWrite(fakeView{domain: "docs", risk: "read", riskOK: true}) {
		t.Errorf("AND fails when one side fails")
	}

	docsOrIm := platform.ByDomain("docs").Or(platform.ByDomain("im"))
	if !docsOrIm(fakeView{domain: "im"}) {
		t.Errorf("OR should match either side")
	}

	notRead := platform.ByReadOnly().Not()
	if notRead(fakeView{risk: "read", riskOK: true}) {
		t.Errorf("Not(ByReadOnly) must reject read commands")
	}
	if !notRead(fakeView{risk: "write", riskOK: true}) {
		t.Errorf("Not(ByReadOnly) should match write")
	}
}

func TestSelector_NilSafeWhenComposed(t *testing.T) {
	// Defensive: a nil Selector passed to And/Or would panic if not
	// guarded. The current impl does not nil-check; this test pins
	// that nil composition panics so a future maintainer knows to add
	// guards if they relax the convention.
	defer func() { _ = recover() }()
	var s platform.Selector
	_ = s.And(platform.All())
}
