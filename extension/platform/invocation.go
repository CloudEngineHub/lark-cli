// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import "time"

// Invocation is the per-command data a Wrapper / Observer receives. It
// is a read-only interface: the framework implementation lives in
// internal/hook and is never visible to plugins, so there is no way
// for plugin code to mutate denial / strict-mode / identity state.
//
// The struct is deliberately NOT a context.Context — it is data only,
// no cancellation. ctx (from the handler signature) carries
// cancellation / timeout / trace propagation.
//
// Accessor semantics:
//
//   - Cmd / Args / Started are populated before the first hook fires
//   - Err is populated for After observers and the post-next portion of
//     a Wrapper (the value the wrapped handler returned)
//   - DeniedByPolicy / DenialLayer / DenialPolicySource are populated by
//     the framework's denial guard before any hook runs
//   - StrictMode / Identity may return ok=false in Before observers if
//     the bootstrap pipeline has not yet resolved them; After observers
//     always see ok=true
type Invocation interface {
	// Cmd is the read-only snapshot of the dispatched command.
	Cmd() CommandView

	// Args is the positional args slice the user invoked the command with.
	Args() []string

	// Started is the wall-clock time the outermost RunE wrapper began.
	Started() time.Time

	// Err is the error the wrapped handler returned. Populated for
	// After observers and the post-next portion of a Wrapper. nil
	// before the handler runs.
	Err() error

	// DeniedByPolicy reports whether the command was rejected by either
	// strict-mode or user-layer policy before the chain reached the
	// hook. Observers fire even for denied commands (audit case); Wrap
	// is physically isolated by the framework so plugins do not need
	// to check this themselves before calling next.
	DeniedByPolicy() bool

	// DenialLayer returns the layer that rejected the command:
	//
	//   ""             - not denied
	//   "strict_mode"  - credential strict-mode
	//   "policy"       - user-layer Rule (Plugin.Restrict() or yaml)
	//
	// Matches the detail.layer field in the envelope so consumers can
	// route recovery logic by this value alone.
	DenialLayer() string

	// DenialPolicySource returns the specific source identifier
	// ("plugin:secaudit", "yaml", "strict-mode") corresponding to the
	// denial. Empty when the command was not denied.
	DenialPolicySource() string

	// StrictMode returns the active credential strict-mode value
	// ("user", "bot", "off"). ok=false signals "not yet resolved".
	StrictMode() (mode string, ok bool)

	// Identity returns the resolved identity ("user"/"bot") for the
	// current command. resolved=false means the framework has not yet
	// resolved identity at the call site.
	Identity() (id string, resolved bool)
}
