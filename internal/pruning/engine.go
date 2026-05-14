// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package pruning is the user-layer command pruning engine. It consumes a
// platform.Rule and the cobra command tree, evaluates each runnable command
// against the rule's four-axis filter (Allow / Deny / MaxRisk / Identities),
// and produces a path -> Decision map. A separate BuildDeniedByPath step
// converts those leaf decisions into a deniedByPath map (with parent-group
// aggregation), which the Apply step consumes to install denyStubs.
//
// This package only implements the user-layer half. Strict-mode is handled
// by cmd/prune.go::applyStrictModeDenials, which consumes the same merged
// deniedByPath produced by the bootstrap pipeline. The two layers share
// the decision-map data structure (internal/policydecision.Denial) but
// keep distinct apply functions so error.type stays accurate.
package pruning

import (
	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/cmdmeta"
	"github.com/larksuite/cli/internal/policydecision"
)

// Decision is the user-layer single-rule evaluation result. Distinct from
// policydecision.Denial: Decision carries Allowed=true/false and the
// rejection reason when Allowed=false; Denial only ever exists when the
// command is rejected. Keeping them separate avoids a perpetually-false
// Allowed field on Denial.
type Decision struct {
	Allowed    bool
	ReasonCode string // "" when Allowed=true
	Reason     string // human-readable
}

// Engine evaluates a Rule against the command tree. It is stateless except
// for the Rule snapshot it was constructed with.
type Engine struct {
	rule *platform.Rule
}

// New returns an Engine bound to a Rule. A nil Rule means "no user-layer
// restriction" -- EvaluateOne always returns Allowed=true.
func New(rule *platform.Rule) *Engine {
	return &Engine{rule: rule}
}

// EvaluateAll walks the command tree and evaluates every **runnable**
// command against the Rule. Pure parent groups (no RunE) are deliberately
// skipped here: their decision is derived from children by
// BuildDeniedByPath. Evaluating groups directly would incorrectly deny
// "docs" under an Allow:["docs/**"] rule (the group's own path "docs"
// does not match the "**"-requiring glob).
//
// Hybrid commands (own RunE plus children) are evaluated as ordinary
// leaves here; the aggregation pass treats them specially.
func (e *Engine) EvaluateAll(root *cobra.Command) map[string]Decision {
	out := map[string]Decision{}
	walkTree(root, func(c *cobra.Command) {
		if !c.Runnable() {
			return
		}
		path := CanonicalPath(c)
		if path == "" {
			return
		}
		out[path] = e.EvaluateOne(c)
	})
	return out
}

// EvaluateOne returns the user-layer decision for a single command. Always
// Allowed=true when the engine has no Rule.
func (e *Engine) EvaluateOne(cmd *cobra.Command) Decision {
	if e.rule == nil {
		return Decision{Allowed: true}
	}
	r := e.rule
	path := CanonicalPath(cmd)

	if IsDiagnosticPath(path) {
		return Decision{Allowed: true}
	}

	// Axis 1: Deny has priority.
	if matchesAny(r.Deny, path) {
		return Decision{
			Allowed:    false,
			ReasonCode: "command_denylisted",
			Reason:     "command denied by rule deny list",
		}
	}

	// Axis 2: Allow gate (empty allow means "no restriction").
	if len(r.Allow) > 0 && !matchesAny(r.Allow, path) {
		return Decision{
			Allowed:    false,
			ReasonCode: "domain_not_allowed",
			Reason:     "command path not in rule allow list",
		}
	}

	// Axis 3: MaxRisk. Unknown command risk is treated as ALLOW per
	// hard-constraint #11 -- we cannot deny here without breaking
	// unannotated legacy commands. The hook layer follows the same rule
	// (risk-based Selectors do not match unknown); a safety-side plugin
	// that wants to cover unannotated commands composes explicitly via
	// ByWrite().Or(ByUnknownRisk()).
	if r.MaxRisk != "" {
		cmdRisk, ok := cmdmeta.Risk(cmd)
		if ok {
			limit, limitOk := platform.RiskRank(r.MaxRisk)
			cmdRank, cmdRankOk := platform.RiskRank(cmdRisk)
			if limitOk && cmdRankOk && cmdRank > limit {
				return Decision{
					Allowed:    false,
					ReasonCode: reasonCodeForRisk(cmdRisk),
					Reason:     "command risk exceeds rule max_risk",
				}
			}
		}
	}

	// Axis 4: Identities. Unknown command identities is treated as ALLOW.
	if len(r.Identities) > 0 {
		cmdIdents := cmdmeta.Identities(cmd)
		if cmdIdents != nil && !hasIntersection(r.Identities, cmdIdents) {
			return Decision{
				Allowed:    false,
				ReasonCode: "identity_mismatch",
				Reason:     "command identities do not intersect rule identities",
			}
		}
	}

	return Decision{Allowed: true}
}

// BuildDeniedByPath converts engine Decisions to a deniedByPath map keyed
// by canonical path. It performs the parent-group aggregation defined in
// the tech doc: a non-runnable parent whose every runnable descendant is
// denied gets an aggregate denial (via policydecision.AggregateChildren);
// hybrid commands (own RunE + children) get one only when both their own
// RunE and all children are denied.
//
// The root command (no parent) is never installed with a denyStub even if
// every child is denied -- the binary entry point must remain dispatchable
// so `--help` and similar remain available.
//
// source / ruleName populate PolicySource and RuleName on the produced
// Denial values, so envelope output can attribute denials.
func BuildDeniedByPath(root *cobra.Command, decisions map[string]Decision, source ResolveSource, ruleName string) map[string]policydecision.Denial {
	out := map[string]policydecision.Denial{}

	sourceLabel := policySourceLabel(source)
	for path, d := range decisions {
		if !d.Allowed {
			out[path] = policydecision.Denial{
				Layer:        policydecision.LayerPruning,
				PolicySource: sourceLabel,
				RuleName:     ruleName,
				ReasonCode:   d.ReasonCode,
				Reason:       d.Reason,
			}
		}
	}

	aggregateParents(root, out)
	return out
}

// aggregateParents recursively examines each parent group. Returns true
// when every runnable descendant beneath cmd (including cmd itself when
// runnable) is denied; in that case the function also inserts an aggregate
// Denial for cmd, unless cmd is the binary root or cmd is already in the
// map (own RunE denial preserved).
//
// "Live" children are those with at least one runnable descendant; pure
// non-runnable placeholders neither count toward "all denied" nor block
// the aggregation.
func aggregateParents(cmd *cobra.Command, denied map[string]policydecision.Denial) bool {
	if cmd == nil {
		return false
	}

	children := cmd.Commands()
	cmdRunnable := cmd.Runnable()
	cmdPath := CanonicalPath(cmd)

	// Pure leaf
	if len(children) == 0 {
		if !cmdRunnable {
			return false // placeholder, doesn't contribute
		}
		_, ok := denied[cmdPath]
		return ok
	}

	// Has children: recurse first, collect direct-child denials for the
	// aggregation message.
	childDenials := make([]policydecision.ChildDenial, 0, len(children))
	liveChildSeen := false
	allLiveChildrenDenied := true
	for _, child := range children {
		childDenied := aggregateParents(child, denied)
		if hasRunnableDescendant(child) {
			liveChildSeen = true
			if !childDenied {
				allLiveChildrenDenied = false
			}
		}
		if cp := CanonicalPath(child); cp != "" {
			if d, ok := denied[cp]; ok {
				childDenials = append(childDenials, policydecision.ChildDenial{Path: cp, Denial: d})
			}
		}
	}

	if !liveChildSeen {
		// No reachable runnable descendant, nothing to aggregate.
		return false
	}

	// Hybrid: own RunE must also be denied for the group to count as denied.
	if cmdRunnable {
		if _, ownDenied := denied[cmdPath]; !ownDenied {
			return false
		}
	}

	if !allLiveChildrenDenied {
		return false
	}

	// Everything reachable below this command is denied. Install the
	// aggregate denyStub if there isn't already an own denial here, and
	// skip the binary root.
	if cmd.HasParent() && cmdPath != "" {
		if _, exists := denied[cmdPath]; !exists {
			policydecision.SortChildren(childDenials)
			denied[cmdPath] = policydecision.AggregateChildren(childDenials)
		}
	}
	return true
}

// hasRunnableDescendant reports whether cmd or any descendant has RunE.
// We use it to ignore pure placeholder branches when aggregating.
func hasRunnableDescendant(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Runnable() {
		return true
	}
	for _, c := range cmd.Commands() {
		if hasRunnableDescendant(c) {
			return true
		}
	}
	return false
}

// policySourceLabel produces the "plugin:foo" / "yaml" / "" label that goes
// into CommandDeniedError.PolicySource and envelope.detail.policy_source.
//
// **Plugin name is included** because plugins live inside the binary and
// their names are part of the implementation contract; an integrator
// debugging a denial wants to know which plugin's Restrict() fired.
//
// **YAML file path is deliberately omitted** -- the envelope is observable
// by agents, CI logs, and other downstream systems, and the path leaks
// the user's home directory (e.g. /Users/alice/.lark-cli/policy.yml).
// The Denial.RuleName field already carries the human-identifier the user
// chose for their rule (yaml's "name:" field), which suffices for
// disambiguation. Use `config policy show` if the absolute path matters
// for a local debugging session.
func policySourceLabel(s ResolveSource) string {
	switch s.Kind {
	case SourcePlugin:
		return "plugin:" + s.Name
	case SourceYAML:
		return "yaml"
	}
	return ""
}

// reasonCodeForRisk picks the canonical reason_code for an exceeds-max-risk
// rejection.
func reasonCodeForRisk(risk platform.Risk) string {
	if risk == platform.RiskWrite || risk == platform.RiskHighRiskWrite {
		return "write_not_allowed"
	}
	return "risk_too_high"
}

// matchesAny reports whether path matches any of the doublestar globs.
// Invalid globs are skipped here -- they're rejected upstream by
// ValidateRule when the rule first enters the system.
func matchesAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, path); err == nil && ok {
			return true
		}
	}
	return false
}

// hasIntersection reports whether two string slices share any element.
// Both slices are short (usually 1-2 identities) so a nested loop beats
// allocating a set.
func hasIntersection(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// walkTree applies fn to every command in the tree, depth-first. Hidden
// commands are visited too -- they can still be invoked.
func walkTree(root *cobra.Command, fn func(*cobra.Command)) {
	if root == nil {
		return
	}
	fn(root)
	for _, c := range root.Commands() {
		walkTree(c, fn)
	}
}
