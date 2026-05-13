// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

// Rule is the declarative pruning rule data structure. yaml files and (once
// the Hook surface lands) Plugin.Restrict() both produce the same Rule.
//
// At any moment there is at most one effective Rule -- the resolver decides
// which source wins (Plugin > yaml > none). This package only defines the
// shape; selection lives in internal/pruning.
//
// The four filter fields are joined by AND. See the engine's Evaluate for
// the full semantics. JSON tags are used by `config policy show`; yaml
// parsing lives in internal/pruning/yaml so the public API does not depend
// on a yaml library.
type Rule struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Allow is a list of doublestar globs (slash-separated paths). An empty
	// slice means "no path restriction"; a non-empty slice means "command
	// path must match at least one glob".
	Allow []string `json:"allow,omitempty"`

	// Deny is a list of doublestar globs. A path that matches any Deny glob
	// is rejected regardless of Allow.
	Deny []string `json:"deny,omitempty"`

	// MaxRisk is the highest allowed risk level (inclusive). Empty string
	// means "no risk restriction". Comparison uses the closed taxonomy
	// read < write < high-risk-write.
	MaxRisk Risk `json:"max_risk,omitempty"`

	// Identities is the allowed identity whitelist. A command passes when
	// the intersection with the command's own supported identities is
	// non-empty. Empty slice means "no identity restriction".
	Identities []Identity `json:"identities,omitempty"`
}
