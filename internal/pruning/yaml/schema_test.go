// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package yaml_test

import (
	"reflect"
	"testing"

	"github.com/larksuite/cli/extension/platform"
	pyaml "github.com/larksuite/cli/internal/pruning/yaml"
)

func TestParse_validRule(t *testing.T) {
	data := []byte(`
name: agent-docs-readonly
description: only-read docs
allow:
  - docs/**
  - contact/**
deny:
  - docs/+update
max_risk: read
identities:
  - user
`)
	rule, err := pyaml.Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	want := &platform.Rule{
		Name:        "agent-docs-readonly",
		Description: "only-read docs",
		Allow:       []string{"docs/**", "contact/**"},
		Deny:        []string{"docs/+update"},
		MaxRisk:     "read",
		Identities:  []string{"user"},
	}
	if !reflect.DeepEqual(rule, want) {
		t.Fatalf("rule = %+v, want %+v", rule, want)
	}
}

// Unknown fields must be rejected so the old binary cannot silently ignore
// new schema additions (forward-compat safeguard).
func TestParse_rejectsUnknownFields(t *testing.T) {
	data := []byte(`
name: x
mystery_field: oh no
`)
	if _, err := pyaml.Parse(data); err == nil {
		t.Fatalf("Parse should reject unknown yaml field 'mystery_field'")
	}
}

// Semantic validation lives in pruning.ValidateRule. Parse only checks
// structural yaml; an invalid max_risk passes through (validation happens
// downstream).
func TestParse_doesNotValidateSemantics(t *testing.T) {
	rule, err := pyaml.Parse([]byte("max_risk: nuclear\n"))
	if err != nil {
		t.Fatalf("structural parse should succeed, got %v", err)
	}
	if rule.MaxRisk != "nuclear" {
		t.Fatalf("MaxRisk = %q, want passed through as-is", rule.MaxRisk)
	}
}

// An entirely empty file is rejected: the resolver should fall back to
// "no rule" by skipping the file in the first place, not by feeding empty
// bytes through Parse.
func TestParse_emptyIsError(t *testing.T) {
	if _, err := pyaml.Parse([]byte{}); err == nil {
		t.Fatalf("Parse should reject empty input; the resolver handles 'no file' separately")
	}
}
