// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package yaml parses a Rule from yaml bytes. It is kept separate from the
// public extension/platform package so that platform stays free of yaml
// library dependencies -- plugins constructing a Rule in Go code never
// import yaml, only the file loader does.
//
// This package does **structural** parsing only (yaml syntax + unknown-field
// rejection). Semantic validation (valid MaxRisk enum, valid identity
// values, valid doublestar glob syntax) is centralised in
// internal/cmdpolicy.ValidateRule so a single contract is enforced regardless
// of whether the Rule came from yaml or from Plugin.Restrict.
package yaml

import (
	"errors"
	"fmt"
	"io"

	gopkgyaml "gopkg.in/yaml.v3"

	"github.com/larksuite/cli/extension/platform"
)

// schema is the internal yaml-tagged shape. Mirrors platform.Rule but lives
// here so the public Rule has no yaml tag baggage.
type schema struct {
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description,omitempty"`
	Allow            []string `yaml:"allow,omitempty"`
	Deny             []string `yaml:"deny,omitempty"`
	MaxRisk          string   `yaml:"max_risk,omitempty"`
	Identities       []string `yaml:"identities,omitempty"`
	AllowUnannotated bool     `yaml:"allow_unannotated,omitempty"`
}

// Parse decodes yaml bytes into a *platform.Rule. Unknown fields are
// rejected so an old binary cannot silently ignore new schema additions
// (forward-compat safeguard).
//
// Semantic validation (MaxRisk taxonomy, identity values, glob syntax) is
// the caller's responsibility -- run the result through
// internal/cmdpolicy.ValidateRule before handing it to the engine.
func Parse(data []byte) (*platform.Rule, error) {
	var s schema
	dec := gopkgyaml.NewDecoder(bytesReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("parse policy yaml: %w", err)
	}

	// Reject multi-document input: yaml.v3 only decodes one document
	// per call, so a stray "---" followed by another document would
	// silently drop the trailing rule.
	var extra schema
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("parse policy yaml: multiple YAML documents are not allowed")
		}
		return nil, fmt.Errorf("parse policy yaml: %w", err)
	}

	idents := make([]platform.Identity, len(s.Identities))
	for i, id := range s.Identities {
		idents[i] = platform.Identity(id)
	}
	return &platform.Rule{
		Name:             s.Name,
		Description:      s.Description,
		Allow:            s.Allow,
		Deny:             s.Deny,
		MaxRisk:          platform.Risk(s.MaxRisk),
		Identities:       idents,
		AllowUnannotated: s.AllowUnannotated,
	}, nil
}
