// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import "github.com/bmatcuk/doublestar/v4"

// Selector picks the commands a hook fires on. A nil Selector is
// equivalent to None() -- safer than an "always-match" default because
// it forces every hook to declare its scope explicitly. Compose
// selectors with And / Or / Not.
type Selector func(cmd CommandView) bool

// All matches every command. Use for audit / metrics observers that
// must run on the whole surface.
func All() Selector { return func(CommandView) bool { return true } }

// None matches no command. Useful as a "disabled" placeholder.
func None() Selector { return func(CommandView) bool { return false } }

// ByDomain matches a command whose Domain() is one of the supplied
// names. Commands with unknown (empty-string) Domain never match this
// selector -- the caller should pair it with a Selector that handles
// unknown explicitly when that case matters.
func ByDomain(domains ...string) Selector {
	wanted := newStringSet(domains)
	return func(cmd CommandView) bool {
		d := cmd.Domain()
		return d != "" && wanted[d]
	}
}

// ByCommandPath matches against the canonical slash-form path. Patterns
// are doublestar globs ("docs/+update", "im/*", "**"). Invalid patterns
// never match; ValidateRule's twin check catches them at the source.
func ByCommandPath(patterns ...string) Selector {
	return func(cmd CommandView) bool {
		path := cmd.Path()
		for _, p := range patterns {
			if ok, err := doublestar.Match(p, path); err == nil && ok {
				return true
			}
		}
		return false
	}
}

// ByIdentity matches when the command's supported identities include
// the supplied id. Unknown identities never match.
func ByIdentity(id string) Selector {
	return func(cmd CommandView) bool {
		for _, x := range cmd.Identities() {
			if x == id {
				return true
			}
		}
		return false
	}
}

// All risk-based selectors below share a single contract: **commands
// without a risk_level annotation (unknown) NEVER match.** Many commands
// in the repo are unannotated; a "unknown = match" semantics would force
// safety / approval plugins to silently cover the whole CLI surface,
// punishing integrators rather than helping. Plugin authors who do want
// to cover unannotated commands should compose explicitly:
//
//	platform.ByWrite().Or(platform.ByUnknownRisk())
//
// This makes the safety widening opt-in and visible at the call site.

// ByExactRisk matches commands whose declared risk level is exactly
// level. Unknown (no annotation) does not match.
func ByExactRisk(level Risk) Selector {
	return func(cmd CommandView) bool {
		v, ok := cmd.Risk()
		return ok && v == level
	}
}

// ByWrite matches commands whose risk is "write" or "high-risk-write".
// Unknown does not match.
func ByWrite() Selector {
	return func(cmd CommandView) bool {
		v, ok := cmd.Risk()
		return ok && (v == RiskWrite || v == RiskHighRiskWrite)
	}
}

// ByReadOnly matches commands whose risk is "read". Unknown does not
// match.
func ByReadOnly() Selector {
	return func(cmd CommandView) bool {
		v, ok := cmd.Risk()
		return ok && v == RiskRead
	}
}

// ByUnknownRisk matches commands that carry no risk_level annotation.
// The intended use is opt-in safety widening via composition, e.g.
//
//	platform.ByWrite().Or(platform.ByUnknownRisk())
//
// for an approval gate that wants to also cover commands a developer
// forgot to annotate. Use sparingly: matching unknown by default would
// rope in every unannotated subcommand including reads.
func ByUnknownRisk() Selector {
	return func(cmd CommandView) bool {
		_, ok := cmd.Risk()
		return !ok
	}
}

// And composes selectors with AND semantics.
func (s Selector) And(other Selector) Selector {
	return func(cmd CommandView) bool {
		return s(cmd) && other(cmd)
	}
}

// Or composes selectors with OR semantics.
func (s Selector) Or(other Selector) Selector {
	return func(cmd CommandView) bool {
		return s(cmd) || other(cmd)
	}
}

// Not negates the selector.
func (s Selector) Not() Selector {
	return func(cmd CommandView) bool {
		return !s(cmd)
	}
}

func newStringSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, x := range items {
		out[x] = true
	}
	return out
}
