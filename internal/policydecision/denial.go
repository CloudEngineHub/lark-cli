// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package policydecision holds the merged-denial decision type that both
// strict-mode and user-layer pruning produce. It lives below both consumers
// (strict-mode apply in cmd/, user-layer engine in internal/pruning) so
// neither has to import the other.
//
// The bootstrap pipeline produces a single deniedByPath map keyed by
// canonical slash path; strict-mode and user-layer apply functions each
// filter the map by Layer and install denyStubs accordingly.
package policydecision

import "sort"

// Layer values match CommandDeniedError.Layer and the error.type field of
// the JSON envelope.
const (
	LayerStrictMode = "strict_mode"
	LayerPruning    = "pruning"
)

// Denial is the merged record for a single rejected command path. It is
// distinct from the user-layer-only pruning.Decision type: Denial only
// exists when the command is rejected (the Allowed bool would be wasted
// here, hence not reusing pruning.Decision).
type Denial struct {
	Layer        string // "strict_mode" | "pruning"
	PolicySource string // "plugin:secaudit" | "yaml:mywork" | "strict-mode" | ""
	RuleName     string // matched Rule.Name (if any)
	ReasonCode   string // closed enum, see tech-doc 5.3
	Reason       string // human-readable
}

// ChildDenial is what AggregateChildren consumes -- it pairs a Denial with
// the child command's path so the aggregate can carry that breakdown for
// envelope.detail.children_denied.
type ChildDenial struct {
	Path   string
	Denial Denial
}

// AggregateChildren produces the parent-group Denial when every child of a
// command group is itself denied. The rules:
//
//   - all children share Layer "strict_mode" -> parent Layer = strict_mode,
//     parent ReasonCode = single child's ReasonCode (if consistent) or
//     "mixed_children_strict_mode" otherwise.
//   - all children share Layer "pruning"     -> parent Layer = pruning,
//     ReasonCode behaves analogously.
//   - mixed layers across children           -> parent Layer = "pruning",
//     ReasonCode = "all_children_denied",
//     PolicySource = "mixed".
//
// Calling with an empty slice returns a zero Denial -- callers should treat
// this as "no aggregation needed".
func AggregateChildren(children []ChildDenial) Denial {
	if len(children) == 0 {
		return Denial{}
	}

	// Detect layer mix and reasonCode consistency.
	layers := map[string]struct{}{}
	reasonCodes := map[string]struct{}{}
	sources := map[string]struct{}{}
	ruleNames := map[string]struct{}{}
	for _, c := range children {
		layers[c.Denial.Layer] = struct{}{}
		reasonCodes[c.Denial.ReasonCode] = struct{}{}
		if c.Denial.PolicySource != "" {
			sources[c.Denial.PolicySource] = struct{}{}
		}
		if c.Denial.RuleName != "" {
			ruleNames[c.Denial.RuleName] = struct{}{}
		}
	}

	// Mixed: layers differ across children. Parent goes to Layer=pruning
	// (the more "user-recoverable" of the two -- swapping policy can flip
	// children, swapping credential cannot).
	if len(layers) > 1 {
		return Denial{
			Layer:        LayerPruning,
			PolicySource: "mixed",
			ReasonCode:   "all_children_denied",
			Reason:       "all child commands are denied (mixed reasons)",
		}
	}

	// Single layer for all children.
	var layer string
	for l := range layers {
		layer = l
	}

	d := Denial{Layer: layer}

	// ReasonCode: collapse when consistent, otherwise prefix with
	// "mixed_children_".
	switch len(reasonCodes) {
	case 1:
		for rc := range reasonCodes {
			d.ReasonCode = rc
		}
	default:
		switch layer {
		case LayerStrictMode:
			d.ReasonCode = "mixed_children_strict_mode"
		default:
			d.ReasonCode = "mixed_children_pruning"
		}
	}

	// PolicySource: identical across children -> carry it; otherwise leave
	// blank (the caller can still see per-child sources via children_denied
	// in the envelope detail).
	if len(sources) == 1 {
		for s := range sources {
			d.PolicySource = s
		}
	}
	if layer == LayerStrictMode {
		d.PolicySource = "strict-mode"
	}

	// RuleName: same idea.
	if len(ruleNames) == 1 {
		for n := range ruleNames {
			d.RuleName = n
		}
	}

	d.Reason = "all child commands are denied"
	return d
}

// SortChildren orders children by Path. The aggregate output of
// AggregateChildren is deterministic regardless of slice order, but tests
// and the envelope's children_denied list want a stable order.
func SortChildren(children []ChildDenial) {
	sort.Slice(children, func(i, j int) bool {
		return children[i].Path < children[j].Path
	})
}
