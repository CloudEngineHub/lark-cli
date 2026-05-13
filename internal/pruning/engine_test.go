// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pruning_test

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/cmdmeta"
	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/policydecision"
	"github.com/larksuite/cli/internal/pruning"
)

// buildTree assembles a tiny realistic tree for engine tests:
//
//	lark-cli (root)
//	├── docs
//	│   ├── +fetch       risk=read    identities=[user,bot]
//	│   ├── +update      risk=write   identities=[user]
//	│   └── +delete-doc  risk=high-risk-write
//	└── im
//	    └── +send        risk=write   identities=[bot]
func buildTree() *cobra.Command {
	root := &cobra.Command{Use: "lark-cli"}

	docs := &cobra.Command{Use: "docs"}
	cmdmeta.SetDomain(docs, "docs")
	root.AddCommand(docs)

	fetch := &cobra.Command{Use: "+fetch", RunE: noop}
	cmdutil.SetRisk(fetch, "read")
	cmdutil.SetSupportedIdentities(fetch, []string{"user", "bot"})
	docs.AddCommand(fetch)

	update := &cobra.Command{Use: "+update", RunE: noop}
	cmdutil.SetRisk(update, "write")
	cmdutil.SetSupportedIdentities(update, []string{"user"})
	docs.AddCommand(update)

	deleteDoc := &cobra.Command{Use: "+delete-doc", RunE: noop}
	cmdutil.SetRisk(deleteDoc, "high-risk-write")
	docs.AddCommand(deleteDoc)

	im := &cobra.Command{Use: "im"}
	cmdmeta.SetDomain(im, "im")
	root.AddCommand(im)

	send := &cobra.Command{Use: "+send", RunE: noop}
	cmdutil.SetRisk(send, "write")
	cmdutil.SetSupportedIdentities(send, []string{"bot"})
	im.AddCommand(send)

	return root
}

func noop(*cobra.Command, []string) error { return nil }

func TestEvaluate_nilRuleAllowsAll(t *testing.T) {
	root := buildTree()
	got := pruning.New(nil).EvaluateAll(root)
	for path, d := range got {
		if !d.Allowed {
			t.Fatalf("nil rule should allow all, got Allowed=false for %s", path)
		}
	}
}

func TestEvaluate_allowGlob(t *testing.T) {
	root := buildTree()
	e := pruning.New(&platform.Rule{
		Allow: []string{"docs/**"},
	})
	got := e.EvaluateAll(root)

	if !got["docs/+fetch"].Allowed {
		t.Errorf("docs/+fetch should be allowed by docs/** glob")
	}
	if got["im/+send"].Allowed {
		t.Errorf("im/+send should NOT be allowed when Allow=docs/**")
	}
	if got["im/+send"].ReasonCode != "domain_not_allowed" {
		t.Errorf("im/+send ReasonCode = %q, want domain_not_allowed",
			got["im/+send"].ReasonCode)
	}
}

func TestEvaluate_denyTakesPriorityOverAllow(t *testing.T) {
	root := buildTree()
	e := pruning.New(&platform.Rule{
		Allow: []string{"docs/**"},
		Deny:  []string{"docs/+delete-doc"},
	})
	got := e.EvaluateAll(root)

	if got["docs/+delete-doc"].Allowed {
		t.Errorf("docs/+delete-doc should be denied by Deny rule")
	}
	if got["docs/+delete-doc"].ReasonCode != "command_denylisted" {
		t.Errorf("ReasonCode = %q, want command_denylisted",
			got["docs/+delete-doc"].ReasonCode)
	}
	if !got["docs/+fetch"].Allowed {
		t.Errorf("docs/+fetch should still be allowed (not in Deny)")
	}
}

func TestEvaluate_maxRiskCutoff(t *testing.T) {
	root := buildTree()
	e := pruning.New(&platform.Rule{
		MaxRisk: "write", // allow read+write, deny high-risk-write
	})
	got := e.EvaluateAll(root)

	if !got["docs/+update"].Allowed {
		t.Errorf("+update (risk=write) should pass MaxRisk=write")
	}
	if !got["docs/+fetch"].Allowed {
		t.Errorf("+fetch (risk=read) should pass MaxRisk=write")
	}
	if got["docs/+delete-doc"].Allowed {
		t.Errorf("+delete-doc (risk=high-risk-write) should fail MaxRisk=write")
	}
	if rc := got["docs/+delete-doc"].ReasonCode; rc != "write_not_allowed" {
		t.Errorf("ReasonCode = %q, want write_not_allowed", rc)
	}
}

// Hard-constraint #11: unknown risk_level means ALLOW. A command without a
// risk annotation must pass even under MaxRisk=read.
func TestEvaluate_unknownRiskIsAllow(t *testing.T) {
	root := &cobra.Command{Use: "lark-cli"}
	docs := &cobra.Command{Use: "docs"}
	root.AddCommand(docs)
	// Note: no SetRisk on this command -> unknown
	orphan := &cobra.Command{Use: "+orphan", RunE: noop}
	docs.AddCommand(orphan)

	e := pruning.New(&platform.Rule{MaxRisk: "read"})
	got := e.EvaluateAll(root)
	if !got["docs/+orphan"].Allowed {
		t.Fatalf("unknown risk must pass MaxRisk=read (constraint #11)")
	}
}

func TestEvaluate_identitiesIntersection(t *testing.T) {
	root := buildTree()
	e := pruning.New(&platform.Rule{
		Identities: []string{"bot"}, // bot-only rule
	})
	got := e.EvaluateAll(root)

	// docs/+fetch has [user, bot] -- intersection includes bot -> ALLOW
	if !got["docs/+fetch"].Allowed {
		t.Errorf("+fetch (identities=user,bot) should intersect bot rule")
	}
	// docs/+update has [user] -- no intersection with bot -> DENY
	if got["docs/+update"].Allowed {
		t.Errorf("+update (identities=user) should fail bot-only rule")
	}
	if got["docs/+update"].ReasonCode != "identity_mismatch" {
		t.Errorf("ReasonCode = %q, want identity_mismatch",
			got["docs/+update"].ReasonCode)
	}
}

// Unknown identities also defaults to ALLOW. A command without
// supportedIdentities passes any identity filter.
func TestEvaluate_unknownIdentitiesIsAllow(t *testing.T) {
	root := &cobra.Command{Use: "lark-cli"}
	cmd := &cobra.Command{Use: "+x", RunE: noop}
	root.AddCommand(cmd)
	// no SetSupportedIdentities

	e := pruning.New(&platform.Rule{Identities: []string{"bot"}})
	got := e.EvaluateAll(root)
	if !got["+x"].Allowed {
		t.Fatalf("unknown identities must pass any identity rule (constraint #11)")
	}
}

// Apply must install denyStubs only on Layer="pruning" entries. A
// "strict_mode" denial in the same map must be left for
// applyStrictModeDenials in cmd/.
func TestApply_onlyTouchesPruningLayer(t *testing.T) {
	root := buildTree()
	denied := map[string]policydecision.Denial{
		"docs/+update": {Layer: "pruning", ReasonCode: "write_not_allowed"},
		"docs/+fetch":  {Layer: "strict_mode", ReasonCode: "identity_not_supported"},
	}

	count := pruning.Apply(root, denied)
	if count != 1 {
		t.Fatalf("Apply count = %d, want 1 (only pruning-layer entries)", count)
	}

	update := findChild(t, root, "docs", "+update")
	if !update.Hidden {
		t.Errorf("+update should be Hidden after Apply")
	}
	if !update.DisableFlagParsing {
		t.Errorf("+update should have DisableFlagParsing=true (constraint #4)")
	}

	// strict-mode entry must NOT have been touched here.
	fetch := findChild(t, root, "docs", "+fetch")
	if fetch.Hidden || fetch.DisableFlagParsing {
		t.Errorf("+fetch (strict_mode layer) should NOT be touched by pruning.Apply")
	}
}

// Calling the denied RunE must produce a typed CommandDeniedError with the
// right Layer/ReasonCode. This is the contract every external consumer
// (agent, integration) depends on.
func TestApply_runEReturnsTypedError(t *testing.T) {
	root := buildTree()
	pruning.Apply(root, map[string]policydecision.Denial{
		"docs/+update": {
			Layer:        "pruning",
			PolicySource: "plugin:secaudit",
			RuleName:     "secaudit-policy",
			ReasonCode:   "write_not_allowed",
			Reason:       "write disabled",
		},
	})

	update := findChild(t, root, "docs", "+update")
	err := update.RunE(update, []string{})
	if err == nil {
		t.Fatalf("denied command should return error")
	}
	var denied *platform.CommandDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("error should be *platform.CommandDeniedError, got %T", err)
	}
	if denied.Layer != "pruning" || denied.ReasonCode != "write_not_allowed" {
		t.Errorf("denial = %+v, want layer=pruning code=write_not_allowed", denied)
	}
	if denied.Path != "docs/+update" {
		t.Errorf("Path = %q, want docs/+update", denied.Path)
	}
	if denied.PolicySource != "plugin:secaudit" || denied.RuleName != "secaudit-policy" {
		t.Errorf("policy source / rule name lost in stub: %+v", denied)
	}
}

func TestApply_emptyMapNoop(t *testing.T) {
	root := buildTree()
	if got := pruning.Apply(root, nil); got != 0 {
		t.Fatalf("nil deniedByPath should yield count=0, got %d", got)
	}
}

// CanonicalPath strips the root and joins with slashes -- the form
// doublestar globs need to work.
func TestCanonicalPath(t *testing.T) {
	root := buildTree()
	update := findChild(t, root, "docs", "+update")
	if got := pruning.CanonicalPath(update); got != "docs/+update" {
		t.Fatalf("CanonicalPath = %q, want docs/+update", got)
	}
	if got := pruning.CanonicalPath(root); got != "lark-cli" {
		t.Fatalf("CanonicalPath(root) = %q, want lark-cli (orphan fallback)", got)
	}
}

// findChild is a test helper: descend a path of cmd.Use names through the
// tree, failing the test if any step is missing.
func findChild(t *testing.T, parent *cobra.Command, names ...string) *cobra.Command {
	t.Helper()
	cur := parent
	for _, n := range names {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Use == n {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("child %q not found under %q", n, cur.Use)
		}
		cur = next
	}
	return cur
}
