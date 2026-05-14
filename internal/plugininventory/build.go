// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package plugininventory

import (
	"strings"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/hook"
)

// PluginSource is the minimum slice of platformhost.PluginInfo we need
// here. Declared as an interface to avoid importing platformhost
// (which itself depends on hook, pruning -- keeping plugininventory at
// a lower level of the dependency graph).
type PluginSource struct {
	Name         string
	Version      string
	Capabilities platform.Capabilities
}

// RuleSource is the minimum slice of pruning.PluginRule we need.
type RuleSource struct {
	PluginName string
	Allow      []string
	Deny       []string
	MaxRisk    string
	Identities []string
	RuleName   string
	Desc       string
}

// Build assembles an Inventory from the parts produced by
// platformhost.InstallAll: the plugin metadata list, the hook registry
// (may be nil when no hooks were registered), and the plugin rules.
//
// Hooks are attributed to plugins by the namespaced name convention:
// each entry's Name starts with "<plugin>.", and we group by the
// leading segment up to the first dot.
func Build(plugins []PluginSource, registry *hook.Registry, rules []RuleSource) *Inventory {
	byPlugin := make(map[string]*PluginEntry, len(plugins))
	out := &Inventory{Plugins: make([]PluginEntry, 0, len(plugins))}
	for _, p := range plugins {
		entry := PluginEntry{
			Name:         p.Name,
			Version:      p.Version,
			Capabilities: NewCapabilitiesView(p.Capabilities),
		}
		out.Plugins = append(out.Plugins, entry)
	}
	for i := range out.Plugins {
		byPlugin[out.Plugins[i].Name] = &out.Plugins[i]
	}

	if registry != nil {
		for _, e := range registry.Observers() {
			if entry := byPlugin[ownerOf(e.Name)]; entry != nil {
				entry.Observers = append(entry.Observers, HookEntry{
					Name: e.Name,
					When: whenLabel(e.When),
				})
			}
		}
		for _, e := range registry.Wrappers() {
			if entry := byPlugin[ownerOf(e.Name)]; entry != nil {
				entry.Wrappers = append(entry.Wrappers, HookEntry{
					Name: e.Name,
				})
			}
		}
		for _, e := range registry.Lifecycles() {
			if entry := byPlugin[ownerOf(e.Name)]; entry != nil {
				entry.Lifecycles = append(entry.Lifecycles, HookEntry{
					Name:  e.Name,
					Event: eventLabel(e.Event),
				})
			}
		}
	}

	for _, r := range rules {
		if entry := byPlugin[r.PluginName]; entry != nil {
			entry.Rule = &RuleView{
				Name:        r.RuleName,
				Description: r.Desc,
				Allow:       r.Allow,
				Deny:        r.Deny,
				MaxRisk:     r.MaxRisk,
				Identities:  r.Identities,
			}
		}
	}
	return out
}

// ownerOf extracts the plugin name from a namespaced hook name. The
// platform forbids "." in plugin names, so the first dot is always the
// namespace separator. Names without a dot are returned as-is (best-
// effort: an unregistered or pre-namespaced legacy hook still surfaces
// under its own name).
func ownerOf(hookName string) string {
	if i := strings.IndexByte(hookName, '.'); i >= 0 {
		return hookName[:i]
	}
	return hookName
}

func whenLabel(w platform.When) string {
	switch w {
	case platform.Before:
		return "Before"
	case platform.After:
		return "After"
	}
	return ""
}

func eventLabel(e platform.LifecycleEvent) string {
	switch e {
	case platform.Startup:
		return "Startup"
	case platform.Shutdown:
		return "Shutdown"
	}
	return ""
}
