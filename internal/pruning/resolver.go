// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pruning

import (
	"errors"
	"fmt"
	"os"

	"github.com/larksuite/cli/extension/platform"
	pyaml "github.com/larksuite/cli/internal/pruning/yaml"
	"github.com/larksuite/cli/internal/vfs"
)

// SourceKind describes which source contributed the active Rule. Surfaced
// by `config policy show` so users can tell at a glance whether their yaml
// is being shadowed by a plugin.
type SourceKind string

const (
	SourcePlugin SourceKind = "plugin"
	SourceYAML   SourceKind = "yaml"
	SourceNone   SourceKind = "none"
)

// ResolveSource is the metadata about which rule won.
type ResolveSource struct {
	Kind SourceKind
	Name string // plugin name when Kind=plugin; file path when Kind=yaml; "" otherwise
}

// PluginRule represents a single Restrict() contribution. The Hook surface
// (next milestone) will collect these via Plugin.Install -> r.Restrict; for
// now the package consumer (Bootstrap pipeline) just hands in the slice.
//
// More than one entry is a configuration error (single-rule policy) -- the
// resolver reports it as a typed error so the bootstrap can abort.
type PluginRule struct {
	PluginName string
	Rule       *platform.Rule
}

// ErrMultipleRestricts is returned when 2+ plugins both contribute a Rule.
// The bootstrap pipeline must treat this as fail-closed (start-up abort);
// resolving by silent priority would mask a configuration mistake.
var ErrMultipleRestricts = errors.New("multiple plugins called Restrict; only one is permitted")

// Resolve picks the active Rule from the configured sources. Precedence:
//
//	plugin contribution  >  yaml file at yamlPath  >  no rule
//
// pluginRules may be nil/empty. yamlPath may be "" (skip yaml).
//
// The chosen Rule is validated through ValidateRule before being returned
// -- bad MaxRisk strings, malformed globs, or unknown identities all
// abort the resolve with a typed error so the bootstrap pipeline can
// honour the plugin's FailurePolicy. A typo in a policy plugin must
// never silently fail-open by reaching the engine.
//
// The returned Rule pointer is owned by the caller; resolver does not
// retain a reference.
func Resolve(pluginRules []PluginRule, yamlPath string) (*platform.Rule, ResolveSource, error) {
	switch len(pluginRules) {
	case 0:
		// fall through to yaml
	case 1:
		rule := pluginRules[0].Rule
		if err := ValidateRule(rule); err != nil {
			return nil, ResolveSource{}, fmt.Errorf("plugin %q rule invalid: %w", pluginRules[0].PluginName, err)
		}
		return rule, ResolveSource{Kind: SourcePlugin, Name: pluginRules[0].PluginName}, nil
	default:
		names := make([]string, len(pluginRules))
		for i, pr := range pluginRules {
			names[i] = pr.PluginName
		}
		return nil, ResolveSource{}, fmt.Errorf("%w: %v", ErrMultipleRestricts, names)
	}

	if yamlPath != "" {
		// vfs.Stat lets callers swap in an in-memory FS for tests. The
		// errors here surface as typed os.ErrNotExist when the file is
		// absent, just like a direct os.ReadFile call would.
		if _, err := vfs.Stat(yamlPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, ResolveSource{Kind: SourceNone}, nil
			}
			return nil, ResolveSource{}, fmt.Errorf("stat policy yaml %q: %w", yamlPath, err)
		}
		data, err := vfs.ReadFile(yamlPath)
		if err != nil {
			return nil, ResolveSource{}, fmt.Errorf("read policy yaml %q: %w", yamlPath, err)
		}
		rule, err := pyaml.Parse(data)
		if err != nil {
			return nil, ResolveSource{}, fmt.Errorf("policy yaml %q: %w", yamlPath, err)
		}
		if err := ValidateRule(rule); err != nil {
			return nil, ResolveSource{}, fmt.Errorf("policy yaml %q: %w", yamlPath, err)
		}
		return rule, ResolveSource{Kind: SourceYAML, Name: yamlPath}, nil
	}

	return nil, ResolveSource{Kind: SourceNone}, nil
}
