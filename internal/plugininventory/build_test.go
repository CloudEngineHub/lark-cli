// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package plugininventory_test

import (
	"context"
	"testing"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/hook"
	"github.com/larksuite/cli/internal/plugininventory"
)

func TestBuild_groupsByPluginName(t *testing.T) {
	plugins := []plugininventory.PluginSource{
		{Name: "a", Version: "1.0", Capabilities: platform.Capabilities{
			Restricts: true, FailurePolicy: platform.FailClosed,
		}},
		{Name: "b", Version: "2.0"},
	}

	r := hook.NewRegistry()
	obs := func(context.Context, *platform.Invocation) {}
	wrap := func(next platform.Handler) platform.Handler { return next }
	lc := func(context.Context, *platform.LifecycleContext) error { return nil }

	r.AddObserver(hook.ObserverEntry{Name: "a.pre", When: platform.Before, Selector: platform.All(), Fn: obs})
	r.AddObserver(hook.ObserverEntry{Name: "a.post", When: platform.After, Selector: platform.All(), Fn: obs})
	r.AddObserver(hook.ObserverEntry{Name: "b.audit", When: platform.Before, Selector: platform.All(), Fn: obs})
	r.AddWrapper(hook.WrapperEntry{Name: "a.approval", Selector: platform.All(), Fn: wrap})
	r.AddLifecycle(hook.LifecycleEntry{Name: "a.boot", Event: platform.Startup, Fn: lc})
	r.AddLifecycle(hook.LifecycleEntry{Name: "b.bye", Event: platform.Shutdown, Fn: lc})

	rules := []plugininventory.RuleSource{
		{PluginName: "a", RuleName: "a-rule", Allow: []string{"docs/**"}, MaxRisk: "read"},
	}

	inv := plugininventory.Build(plugins, r, rules)

	if got := len(inv.Plugins); got != 2 {
		t.Fatalf("Plugins len = %d, want 2", got)
	}
	a := findPlugin(inv, "a")
	b := findPlugin(inv, "b")
	if a == nil || b == nil {
		t.Fatalf("missing entries: a=%v b=%v", a, b)
	}

	if got := len(a.Observers); got != 2 {
		t.Errorf("a.Observers = %d, want 2", got)
	}
	if got := len(a.Wrappers); got != 1 {
		t.Errorf("a.Wrappers = %d, want 1", got)
	}
	if got := len(a.Lifecycles); got != 1 {
		t.Errorf("a.Lifecycles = %d, want 1", got)
	}
	if a.Rule == nil || a.Rule.Name != "a-rule" {
		t.Errorf("a.Rule = %+v, want name a-rule", a.Rule)
	}
	if a.Capabilities.FailurePolicy != "FailClosed" {
		t.Errorf("a.Capabilities.FailurePolicy = %q, want FailClosed", a.Capabilities.FailurePolicy)
	}

	if got := len(b.Observers); got != 1 {
		t.Errorf("b.Observers = %d, want 1 (only b.audit)", got)
	}
	if b.Rule != nil {
		t.Errorf("b.Rule = %+v, want nil (b did not call Restrict)", b.Rule)
	}
	if b.Capabilities.FailurePolicy != "FailOpen" {
		t.Errorf("b.Capabilities.FailurePolicy = %q, want FailOpen (zero value)", b.Capabilities.FailurePolicy)
	}
}

func TestBuild_emptyRegistry(t *testing.T) {
	inv := plugininventory.Build(nil, nil, nil)
	if got := len(inv.Plugins); got != 0 {
		t.Errorf("Plugins len = %d, want 0", got)
	}
}

func findPlugin(inv *plugininventory.Inventory, name string) *plugininventory.PluginEntry {
	for i := range inv.Plugins {
		if inv.Plugins[i].Name == name {
			return &inv.Plugins[i]
		}
	}
	return nil
}
