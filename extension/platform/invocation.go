// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import "time"

// Invocation carries the per-command context a Wrapper or Observer needs.
// Cmd is the read-only snapshot taken before any RunE replacement (see
// CommandView); Args is the actual user input; Started is when the
// outermost RunE wrapper began. Err is populated for After hooks and
// the post-next portion of a Wrapper.
//
// The struct is deliberately NOT a context.Context -- it is data only,
// no cancellation. ctx (from the function signature) is the
// context.Context for cancellation/timeout/trace propagation.
//
// Implementation note: the lazy fields (DeniedByPolicy, Identity, etc.)
// are populated by the framework before any hook fires. Plugins must
// not depend on these being non-zero at construction; they always read
// through the accessor methods which centralise the "is this populated
// yet?" logic.
type Invocation struct {
	Cmd     CommandView
	Args    []string
	Started time.Time
	Err     error

	// Unexported state populated by the framework. Plugins read it via
	// the methods below; direct field access is impossible.
	deniedByPolicy bool
	denialLayer    string // "strict_mode" / "pruning" / ""
	denialSource   string // "plugin:secaudit" / "yaml" / "strict-mode" / ""

	// strictMode is the resolved credential strict-mode value, or
	// the empty string when no strict-mode is active. We do not use
	// a separate "resolved?" bool: the StrictMode() accessor returns
	// ok=false when the lifecycle has not yet resolved this.
	strictMode        string
	strictModeKnown   bool

	identity        string
	identityResolved bool
}

// DeniedByPolicy reports whether the command was rejected by either
// strict-mode or user-layer pruning before the chain reached the
// hook. Observers fire even for denied commands (audit case); Wrap is
// physically isolated by the framework so plugins do not need to check
// this themselves before calling next.
func (inv *Invocation) DeniedByPolicy() bool { return inv.deniedByPolicy }

// DenialLayer returns the layer that rejected the command:
//
//	""             - not denied
//	"strict_mode"  - credential strict-mode
//	"pruning"      - user-layer Rule (Plugin.Restrict() or yaml)
//
// Matches the error.type field in the envelope so consumers can route
// recovery logic by this value alone.
func (inv *Invocation) DenialLayer() string { return inv.denialLayer }

// DenialPolicySource returns the specific source identifier
// ("plugin:secaudit", "yaml", "strict-mode") corresponding to the
// denial. Empty when the command was not denied.
func (inv *Invocation) DenialPolicySource() string { return inv.denialSource }

// StrictMode returns the active credential strict-mode value
// ("user", "bot", "off"). ok=false signals "not yet resolved" -- the
// Bootstrap pipeline resolves strict-mode before any hook fires, so in
// practice hooks always see ok=true; the bool exists to keep this
// safe under future reordering.
func (inv *Invocation) StrictMode() (mode string, ok bool) {
	return inv.strictMode, inv.strictModeKnown
}

// Identity returns the resolved identity ("user"/"bot") for the
// current command. resolved=false means the framework has not yet
// resolved identity at the call site (Before observers and Wrap entry
// may see this; After observers always see resolved=true).
func (inv *Invocation) Identity() (id string, resolved bool) {
	return inv.identity, inv.identityResolved
}

// --- internal setters (lower-case, package-internal) ---
//
// Public callers cannot mutate these fields; the framework uses
// targeted helpers exposed only to internal/hook.

// SetDenial is called by the framework before the hook chain runs.
// Exported with "Internal" prefix to mark "framework-only" intent; it
// is technically importable but lives outside the contract surface.
// Renaming or removing it is not a breaking change.
func (inv *Invocation) InternalSetDenial(deniedByPolicy bool, layer, source string) {
	inv.deniedByPolicy = deniedByPolicy
	inv.denialLayer = layer
	inv.denialSource = source
}

// InternalSetStrictMode populates the strict-mode accessor.
func (inv *Invocation) InternalSetStrictMode(mode string, known bool) {
	inv.strictMode = mode
	inv.strictModeKnown = known
}

// InternalSetIdentity populates the identity accessor.
func (inv *Invocation) InternalSetIdentity(id string, resolved bool) {
	inv.identity = id
	inv.identityResolved = resolved
}
