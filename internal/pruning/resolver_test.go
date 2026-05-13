// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pruning_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/pruning"
)

func TestResolve_singlePluginWins(t *testing.T) {
	rule := &platform.Rule{Name: "secaudit"}
	got, src, err := pruning.Resolve([]pruning.PluginRule{
		{PluginName: "secaudit", Rule: rule},
	}, "")
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if got != rule || src.Kind != pruning.SourcePlugin || src.Name != "secaudit" {
		t.Fatalf("Resolve = (%v, %+v)", got, src)
	}
}

func TestResolve_pluginShadowsYaml(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "policy.yml")
	if err := os.WriteFile(yamlPath, []byte("name: from-yaml\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	pluginRule := &platform.Rule{Name: "from-plugin"}
	got, src, err := pruning.Resolve(
		[]pruning.PluginRule{{PluginName: "secaudit", Rule: pluginRule}},
		yamlPath,
	)
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if got.Name != "from-plugin" || src.Kind != pruning.SourcePlugin {
		t.Fatalf("plugin should shadow yaml, got %+v / %+v", got, src)
	}
}

func TestResolve_yamlWhenNoPlugin(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "policy.yml")
	if err := os.WriteFile(yamlPath, []byte("name: from-yaml\nmax_risk: read\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	got, src, err := pruning.Resolve(nil, yamlPath)
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if got.Name != "from-yaml" || src.Kind != pruning.SourceYAML {
		t.Fatalf("yaml should win when no plugin, got %+v / %+v", got, src)
	}
}

func TestResolve_missingYamlIsNoRule(t *testing.T) {
	got, src, err := pruning.Resolve(nil, "/nonexistent/policy.yml")
	if err != nil {
		t.Fatalf("missing yaml should not error, got %v", err)
	}
	if got != nil || src.Kind != pruning.SourceNone {
		t.Fatalf("expected (nil, SourceNone), got (%v, %+v)", got, src)
	}
}

// Two plugins both contributing a Rule must produce the typed error so the
// bootstrap pipeline aborts (hard-constraint #7).
func TestResolve_multipleRestrictIsError(t *testing.T) {
	_, _, err := pruning.Resolve([]pruning.PluginRule{
		{PluginName: "a", Rule: &platform.Rule{Name: "a"}},
		{PluginName: "b", Rule: &platform.Rule{Name: "b"}},
	}, "")
	if !errors.Is(err, pruning.ErrMultipleRestricts) {
		t.Fatalf("err = %v, want ErrMultipleRestricts", err)
	}
}

func TestResolve_emptyEverythingIsNone(t *testing.T) {
	got, src, err := pruning.Resolve(nil, "")
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if got != nil || src.Kind != pruning.SourceNone {
		t.Fatalf("expected (nil, SourceNone), got (%v, %+v)", got, src)
	}
}
