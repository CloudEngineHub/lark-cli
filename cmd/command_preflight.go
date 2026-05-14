// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"strings"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/registry"
	"github.com/larksuite/cli/shortcuts"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const maxShortcutHintItems = 8

func validateCommandInvocation(root *cobra.Command, args []string) error {
	if token, ok := firstRootCommandToken(root, args); ok {
		if _, found := findTopLevelCommand(root, token); !found {
			return unknownCommandError(root, token)
		}
	}

	// Traverse can still fail for ordinary Cobra errors (for example unknown
	// flags). Leave those on the normal Execute path so this preflight only
	// replaces silent parent-help fallbacks with structured validation errors.
	cmd, remaining, err := root.Traverse(args)
	if err != nil {
		return nil //nolint:nilerr // defer to RootCmd.Execute* for normal Cobra errors
	}
	if cmd == nil || len(remaining) == 0 {
		return nil
	}
	unknown := remaining[0]
	if !strings.HasPrefix(unknown, "+") {
		return nil
	}
	available := availableShortcutCommands(cmd.Name())
	if len(available) == 0 {
		return nil
	}

	commandPath := cmd.CommandPath()
	return &output.ExitError{
		Code: output.ExitValidation,
		Detail: &output.ErrDetail{
			Type:    "unknown_shortcut",
			Message: fmt.Sprintf("shortcut %q is not supported for %q", unknown, commandPath),
			Hint:    fmt.Sprintf("available shortcuts: %s; run `%s --help` to see all", shortcutHintList(available), commandPath),
			Detail: map[string]interface{}{
				"shortcut":            unknown,
				"service":             cmd.Name(),
				"available_shortcuts": available,
			},
		},
	}
}

func firstRootCommandToken(root *cobra.Command, args []string) (string, bool) {
	flags := root.PersistentFlags()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
		if arg == "--help" || arg == "-h" {
			return "", false
		}
		if strings.HasPrefix(arg, "--") {
			flagName := strings.TrimPrefix(arg, "--")
			hasInlineValue := false
			if idx := strings.Index(flagName, "="); idx >= 0 {
				flagName = flagName[:idx]
				hasInlineValue = true
			}
			flag := flags.Lookup(flagName)
			if flag == nil {
				return "", false
			}
			if !hasInlineValue && flagConsumesValue(flag) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			if len(arg) != 2 {
				return "", false
			}
			flag := flags.ShorthandLookup(arg[1:])
			if flag == nil {
				return "", false
			}
			if flagConsumesValue(flag) {
				i++
			}
			continue
		}
		return arg, true
	}
	return "", false
}

func flagConsumesValue(flag *pflag.Flag) bool {
	return flag != nil && flag.NoOptDefVal == ""
}

func findTopLevelCommand(root *cobra.Command, token string) (*cobra.Command, bool) {
	if token == "help" {
		return nil, true
	}
	for _, cmd := range root.Commands() {
		if cmd.Name() == token || cmd.HasAlias(token) {
			return cmd, true
		}
	}
	return nil, false
}

func unknownCommandError(root *cobra.Command, token string) error {
	available := availableTopLevelCommands(root)
	services := availableServiceCommands(root)
	commandPath := root.CommandPath()
	return &output.ExitError{
		Code: output.ExitValidation,
		Detail: &output.ErrDetail{
			Type:    "unknown_command",
			Message: fmt.Sprintf("command %q is not supported for %q", token, commandPath),
			Hint:    fmt.Sprintf("available commands: %s; run `%s --help` to see all", shortcutHintList(available), commandPath),
			Detail: map[string]interface{}{
				"command":            token,
				"scope":              "root",
				"available_commands": available,
				"available_services": services,
			},
		},
	}
}

func availableTopLevelCommands(root *cobra.Command) []string {
	out := make([]string, 0)
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		out = append(out, cmd.Name())
	}
	return out
}

func availableServiceCommands(root *cobra.Command) []string {
	registered := map[string]struct{}{}
	for _, cmd := range root.Commands() {
		registered[cmd.Name()] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, project := range registry.ListFromMetaProjects() {
		spec := registry.LoadFromMeta(project)
		if spec == nil {
			continue
		}
		name := registry.GetStrFromMap(spec, "name")
		if name == "" {
			continue
		}
		if _, ok := registered[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func availableShortcutCommands(service string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, shortcut := range shortcuts.AllShortcuts() {
		if shortcut.Service != service || shortcut.Hidden {
			continue
		}
		if _, ok := seen[shortcut.Command]; ok {
			continue
		}
		seen[shortcut.Command] = struct{}{}
		out = append(out, shortcut.Command)
	}
	return out
}

func shortcutHintList(commands []string) string {
	if len(commands) <= maxShortcutHintItems {
		return strings.Join(commands, ", ")
	}
	head := strings.Join(commands[:maxShortcutHintItems], ", ")
	return fmt.Sprintf("%s, ...and %d more", head, len(commands)-maxShortcutHintItems)
}
