// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/pruning"
)

func newPolicyTestFactory() (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := &cmdutil.Factory{
		IOStreams: cmdutil.NewIOStreams(nil, out, errOut),
	}
	return f, out, errOut
}

// `config policy show` reads the active policy recorded by bootstrap.
// When nothing is recorded the command must still produce a JSON
// envelope with source=none and a note explaining the missing context.
func TestConfigPolicyShow_NoActivePolicy(t *testing.T) {
	pruning.ResetActiveForTesting()
	t.Cleanup(pruning.ResetActiveForTesting)

	f, out, _ := newPolicyTestFactory()
	if err := runConfigPolicyShow(f); err != nil {
		t.Fatalf("show: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not json: %v\n%s", err, out.String())
	}
	if got["source"] != "none" {
		t.Errorf("source = %v, want none", got["source"])
	}
	if got["note"] == "" || got["note"] == nil {
		t.Errorf("expected explanatory note when no policy recorded")
	}
}

// When bootstrap recorded an active plugin Rule, `show` emits the rule
// plus its source. yaml_shadowed is true when a yaml file exists but a
// plugin overrode it; verified separately below.
func TestConfigPolicyShow_PluginActive(t *testing.T) {
	pruning.ResetActiveForTesting()
	t.Cleanup(pruning.ResetActiveForTesting)

	rule := &platform.Rule{
		Name:    "secaudit",
		Allow:   []string{"docs/**"},
		MaxRisk: "read",
	}
	pruning.SetActive(&pruning.ActivePolicy{
		Rule: rule,
		Source: pruning.ResolveSource{
			Kind: pruning.SourcePlugin,
			Name: "secaudit",
		},
		DeniedPaths: 42,
	})

	f, out, _ := newPolicyTestFactory()
	if err := runConfigPolicyShow(f); err != nil {
		t.Fatalf("show: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not json: %v\n%s", err, out.String())
	}
	if got["source"] != "plugin" {
		t.Errorf("source = %v, want plugin", got["source"])
	}
	if got["source_name"] != "secaudit" {
		t.Errorf("source_name = %v, want secaudit", got["source_name"])
	}
	// json.Unmarshal returns float64 for numbers.
	if got["denied_paths"] != float64(42) {
		t.Errorf("denied_paths = %v, want 42", got["denied_paths"])
	}
	ruleMap, ok := got["rule"].(map[string]any)
	if !ok {
		t.Fatalf("rule field missing or wrong type")
	}
	if ruleMap["name"] != "secaudit" {
		t.Errorf("rule.name = %v", ruleMap["name"])
	}
}

// When a yaml file exists AND a plugin Rule won, show should warn the
// user "yaml IGNORED" so they're not surprised that their yaml is
// inert.
func TestConfigPolicyShow_YamlShadowedWarning(t *testing.T) {
	pruning.ResetActiveForTesting()
	t.Cleanup(pruning.ResetActiveForTesting)

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "policy.yml")
	if err := os.WriteFile(yamlPath, []byte("name: shadowed\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	pruning.SetActive(&pruning.ActivePolicy{
		Rule: &platform.Rule{Name: "plug"},
		Source: pruning.ResolveSource{
			Kind: pruning.SourcePlugin,
			Name: "plug",
		},
		YAMLPath: yamlPath,
	})

	f, _, errOut := newPolicyTestFactory()
	if err := runConfigPolicyShow(f); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("yaml IGNORED")) {
		t.Errorf("expected 'yaml IGNORED' warning, got: %q", errOut.String())
	}
}

// `config policy validate <path>` must succeed for a well-formed file.
func TestConfigPolicyValidate_ValidYaml(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "policy.yml")
	if err := os.WriteFile(p, []byte(`name: ok
allow: ["docs/**"]
max_risk: read
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, out, _ := newPolicyTestFactory()
	if err := runConfigPolicyValidate(f, p); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not json: %v", err)
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["rule_name"] != "ok" {
		t.Errorf("rule_name = %v", got["rule_name"])
	}
}

// Validate must reject a malformed file with a structured error so CI
// pipelines can parse the result.
func TestConfigPolicyValidate_InvalidYamlRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "policy.yml")
	if err := os.WriteFile(p, []byte("max_risk: nukem\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, _, _ := newPolicyTestFactory()
	err := runConfigPolicyValidate(f, p)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

// Missing file is a validation error too (not a panic).
func TestConfigPolicyValidate_MissingFileRejected(t *testing.T) {
	f, _, _ := newPolicyTestFactory()
	err := runConfigPolicyValidate(f, "/nonexistent/policy.yml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
