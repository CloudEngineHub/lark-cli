// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmdpolicy

import (
	"sync"

	"github.com/larksuite/cli/extension/platform"
)

// ActivePolicy is the resolved user-layer policy after applyUserPolicyPruning
// has run during bootstrap. `lark-cli config policy show` reads this to
// answer "what rule is currently in effect, and how many commands does
// it hide?".
//
// Set once at bootstrap time; consumed read-only thereafter.
type ActivePolicy struct {
	Rule        *platform.Rule
	Source      ResolveSource
	YAMLPath    string // path examined, populated even when yaml was shadowed by a plugin Rule
	DeniedPaths int    // number of commands the engine marked as denied (post-aggregation)
}

var (
	activeMu     sync.RWMutex
	activePolicy *ActivePolicy
)

// SetActive records the policy that ends up applied. Called exactly once
// per process from cmd/policy.go::applyUserPolicyPruning. The mutex is
// belt-and-braces in case future test paths interleave with bootstrap.
func SetActive(p *ActivePolicy) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if p == nil {
		activePolicy = nil
		return
	}
	cp := *p
	activePolicy = &cp
}

// GetActive returns a copy of the recorded policy, or nil if bootstrap
// has not finished or no rule applied.
func GetActive() *ActivePolicy {
	activeMu.RLock()
	defer activeMu.RUnlock()
	if activePolicy == nil {
		return nil
	}
	cp := *activePolicy
	return &cp
}

// ResetActiveForTesting clears the recorded policy. Tests must call this
// in t.Cleanup when they exercise the bootstrap path.
func ResetActiveForTesting() {
	activeMu.Lock()
	defer activeMu.Unlock()
	activePolicy = nil
}
