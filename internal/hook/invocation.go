// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package hook

import (
	"time"

	"github.com/larksuite/cli/extension/platform"
)

// invocation is the framework-side concrete implementation of
// platform.Invocation. All setters are unexported so plugin code
// (which only sees the platform.Invocation interface) cannot mutate
// state.
//
// The "denial" / "strict_mode" / "identity" fields are populated by
// the framework's bootstrap pipeline before any hook fires; plugins
// only read them through the interface.
type invocation struct {
	cmd     platform.CommandView
	args    []string
	started time.Time
	err     error

	denied bool
	layer  string
	source string

	strictMode      string
	strictModeKnown bool

	identity         string
	identityResolved bool
}

// newInvocation copies args so the read-only platform.Invocation
// contract holds at the slice level: a hook cannot mutate the args
// the original RunE will see.
func newInvocation(cmd platform.CommandView, args []string) *invocation {
	argsCopy := append([]string(nil), args...)
	return &invocation{
		cmd:     cmd,
		args:    argsCopy,
		started: time.Now(),
	}
}

// --- platform.Invocation read interface ---

func (i *invocation) Cmd() platform.CommandView { return i.cmd }

// Args returns a fresh copy every call; see newInvocation.
func (i *invocation) Args() []string {
	out := make([]string, len(i.args))
	copy(out, i.args)
	return out
}
func (i *invocation) Started() time.Time { return i.started }
func (i *invocation) Err() error         { return i.err }

func (i *invocation) DeniedByPolicy() bool { return i.denied }
func (i *invocation) DenialLayer() string  { return i.layer }
func (i *invocation) DenialPolicySource() string {
	return i.source
}

func (i *invocation) StrictMode() (string, bool) { return i.strictMode, i.strictModeKnown }
func (i *invocation) Identity() (string, bool)   { return i.identity, i.identityResolved }

// --- framework-internal setters (unexported) ---

func (i *invocation) setDenial(layer, source string) {
	i.denied = true
	i.layer = layer
	i.source = source
}

// StrictMode and Identity setters are intentionally absent in V1: the
// framework does not yet plumb either value to the invocation, and
// platform.Invocation.StrictMode() / Identity() therefore return zero
// values. Add the setters when the bootstrap pipeline starts resolving
// them.

func (i *invocation) setErr(err error) {
	i.err = err
}
