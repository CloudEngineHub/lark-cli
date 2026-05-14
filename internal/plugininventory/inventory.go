// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package plugininventory holds a runtime-readable snapshot of the
// plugins that successfully installed during bootstrap. It powers
// diagnostic commands (config plugins show) without forcing them to
// re-call plugin methods at display time.
//
// The snapshot is built once, after platformhost.InstallAll commits,
// and read-only thereafter. Mutex is belt-and-braces for tests that
// reset state between cases.
package plugininventory

import (
	"sync"

	"github.com/larksuite/cli/extension/platform"
)

// HookEntry is the displayable form of one registered hook.
type HookEntry struct {
	Name  string `json:"name"`
	When  string `json:"when,omitempty"`  // observers only
	Event string `json:"event,omitempty"` // lifecycle only
}

// PluginEntry collects everything one plugin contributed.
type PluginEntry struct {
	Name         string
	Version      string
	Capabilities CapabilitiesView

	// Rule is non-nil only when the plugin called r.Restrict.
	Rule *RuleView

	Observers  []HookEntry
	Wrappers   []HookEntry
	Lifecycles []HookEntry
}

// CapabilitiesView mirrors platform.Capabilities for display. We keep a
// separate struct so the JSON shape stays under our control and does
// not drift with extension/platform.
type CapabilitiesView struct {
	Restricts          bool   `json:"restricts"`
	FailurePolicy      string `json:"failure_policy"`
	RequiredCLIVersion string `json:"required_cli_version,omitempty"`
}

// NewCapabilitiesView converts a platform.Capabilities value into the
// display struct.
func NewCapabilitiesView(c platform.Capabilities) CapabilitiesView {
	return CapabilitiesView{
		Restricts:          c.Restricts,
		FailurePolicy:      failurePolicyLabel(c.FailurePolicy),
		RequiredCLIVersion: c.RequiredCLIVersion,
	}
}

func failurePolicyLabel(p platform.FailurePolicy) string {
	switch p {
	case platform.FailOpen:
		return "FailOpen"
	case platform.FailClosed:
		return "FailClosed"
	}
	return ""
}

// RuleView is the displayable form of a Plugin.Restrict contribution.
type RuleView struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Allow       []string `json:"allow"`
	Deny        []string `json:"deny"`
	MaxRisk     string   `json:"max_risk"`
	Identities  []string `json:"identities"`
}

// Inventory is the full snapshot.
type Inventory struct {
	Plugins []PluginEntry
}

var (
	mu     sync.RWMutex
	active *Inventory
)

// SetActive records the inventory built at bootstrap. Called once from
// cmd/policy.go after install + wireHooks complete.
func SetActive(inv *Inventory) {
	mu.Lock()
	defer mu.Unlock()
	if inv == nil {
		active = nil
		return
	}
	cp := *inv
	active = &cp
}

// GetActive returns a copy of the inventory, or nil if bootstrap has
// not finished.
func GetActive() *Inventory {
	mu.RLock()
	defer mu.RUnlock()
	if active == nil {
		return nil
	}
	cp := *active
	return &cp
}

// ResetForTesting clears the snapshot. Tests must call this in cleanup
// when they exercise the bootstrap path.
func ResetForTesting() {
	mu.Lock()
	defer mu.Unlock()
	active = nil
}
