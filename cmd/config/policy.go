// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/pruning"
	pyaml "github.com/larksuite/cli/internal/pruning/yaml"
)

// NewCmdConfigPolicy returns the `config policy` group. Subcommands:
//
//	show              - print the resolved user-layer Rule + source + denied count
//	validate <path>   - parse + validate a yaml policy file without applying it
//
// Both commands write a structured JSON envelope so AI agents and CI
// integrations can parse the result.
func NewCmdConfigPolicy(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect and validate user-layer command policy",
	}
	cmd.AddCommand(newCmdConfigPolicyShow(f))
	cmd.AddCommand(newCmdConfigPolicyValidate(f))
	return cmd
}

func newCmdConfigPolicyShow(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the active user-layer policy (Plugin.Restrict / yaml / none)",
		Long: `Print the policy currently in effect after bootstrap, including:

  - source: "plugin:<name>" / "yaml" / "none"
  - rule:   the resolved Rule (Allow / Deny / MaxRisk / Identities)
  - yaml_path:    the file location that was examined (informational)
  - yaml_shadowed: true when a plugin Restrict overrides the yaml

A "denied_paths" count reflects the number of commands the engine
marked as denied after father-group aggregation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigPolicyShow(f)
		},
	}
}

func runConfigPolicyShow(f *cmdutil.Factory) error {
	active := pruning.GetActive()
	if active == nil {
		// Bootstrap not yet recorded -- happens when the command is
		// invoked from a context that bypassed buildInternal (only test
		// shells should hit this).
		output.PrintJson(f.IOStreams.Out, map[string]any{
			"source": string(pruning.SourceNone),
			"note":   "no policy recorded; bootstrap did not run pruning",
		})
		return nil
	}

	out := map[string]any{
		"source":       string(active.Source.Kind),
		"source_name":  active.Source.Name,
		"yaml_path":    active.YAMLPath,
		"denied_paths": active.DeniedPaths,
	}
	if active.Rule != nil {
		out["rule"] = map[string]any{
			"name":        active.Rule.Name,
			"description": active.Rule.Description,
			"allow":       active.Rule.Allow,
			"deny":        active.Rule.Deny,
			"max_risk":    active.Rule.MaxRisk,
			"identities":  active.Rule.Identities,
		}
	}
	// Surface the yaml-shadowed case so a user wondering "why is my
	// yaml ignored?" sees it immediately.
	if active.Source.Kind == pruning.SourcePlugin && active.YAMLPath != "" {
		if _, err := os.Stat(active.YAMLPath); err == nil {
			out["yaml_shadowed"] = true
			fmt.Fprintln(f.IOStreams.ErrOut,
				"note: a plugin contributed Restrict(); yaml IGNORED")
		}
	}
	output.PrintJson(f.IOStreams.Out, out)
	return nil
}

func newCmdConfigPolicyValidate(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a yaml policy file (parse + schema + glob checks) without applying it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigPolicyValidate(f, args[0])
		},
	}
}

func runConfigPolicyValidate(f *cmdutil.Factory, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return output.Errorf(output.ExitValidation, "validation",
			"read policy yaml %q: %v", path, err)
	}
	rule, err := pyaml.Parse(data)
	if err != nil {
		return output.Errorf(output.ExitValidation, "validation",
			"parse policy yaml %q: %v", path, err)
	}
	if err := pruning.ValidateRule(rule); err != nil {
		return output.Errorf(output.ExitValidation, "validation",
			"invalid rule in %q: %v", path, err)
	}
	output.PrintJson(f.IOStreams.Out, map[string]any{
		"ok":        true,
		"path":      path,
		"rule_name": rule.Name,
		"allow":     rule.Allow,
		"deny":      rule.Deny,
		"max_risk":  rule.MaxRisk,
	})
	return nil
}
