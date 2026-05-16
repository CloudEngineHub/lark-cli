// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package yaml_test

import (
	"reflect"
	"testing"

	"github.com/larksuite/cli/extension/platform"
	pyaml "github.com/larksuite/cli/internal/cmdpolicy/yaml"
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
		Identities:  []platform.Identity{"user"},
	}
	if !reflect.DeepEqual(rule, want) {
		t.Fatalf("rule = %+v, want %+v", rule, want)
	}
}

// allow_unannotated is documented in the README / author guide as the
// gradual-adoption opt-in. The yaml schema must carry it through to
// platform.Rule, otherwise a user following the docs would either hit
// "unknown field" (under KnownFields strict mode) or silently lose the
// opt-in and end up with a safer-but-broken policy.
func TestParse_allowUnannotatedPassesThrough(t *testing.T) {
	data := []byte(`
name: agent-readonly
max_risk: read
allow_unannotated: true
`)
	rule, err := pyaml.Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if !rule.AllowUnannotated {
		t.Fatalf("AllowUnannotated = false, want true (yaml field must propagate)")
	}
	if rule.MaxRisk != "read" || rule.Name != "agent-readonly" {
		t.Errorf("other fields lost: %+v", rule)
	}
}

// Default is false when the key is absent: pin the fail-closed default so
// future schema edits cannot accidentally flip it.
func TestParse_allowUnannotatedDefaultsFalse(t *testing.T) {
	data := []byte(`
name: x
max_risk: read
`)
	rule, err := pyaml.Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if rule.AllowUnannotated {
		t.Fatalf("AllowUnannotated must default to false when key is absent")
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

// Semantic validation lives in cmdpolicy.ValidateRule. Parse only checks
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

// A stray "---" separator followed by another document would silently
// drop the trailing rule if yaml.v3 stopped after the first Decode.
// Parse must reject multi-document input so the operator can't typo a
// separator and end up with an unintentionally empty policy.
func TestParse_rejectsMultipleDocuments(t *testing.T) {
	data := []byte(`name: first
max_risk: read
---
name: second
max_risk: write
`)
	if _, err := pyaml.Parse(data); err == nil {
		t.Fatalf("Parse should reject multi-document YAML input")
	}
}
