// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmdpolicy

import (
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/vfs"
)

// CanonicalPath returns the rootless slash-separated path used everywhere in
// the pruning framework. Cobra's CommandPath() yields space-separated
// segments ("lark-cli docs +update"); doublestar globs ("docs/**") require
// slashes, so all internal lookups go through this conversion.
//
// Algorithm:
//
//  1. Collect cmd.Use first words from the command up to (but not including)
//     the root, in reverse order.
//  2. Reverse the collection and join with "/".
//
// The root (the binary's own command, no parent) is stripped. For a command
// with no parent, the returned path is just its own Use word.
func CanonicalPath(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	for c := cmd; c != nil && c.HasParent(); c = c.Parent() {
		parts = append(parts, useName(c))
	}
	// reverse
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	if len(parts) == 0 {
		// orphan command -- return its own name so callers still see
		// something stable.
		return useName(cmd)
	}
	return strings.Join(parts, "/")
}

// useName extracts the first word of cmd.Use ("update [flags] <doc>" -> "update").
func useName(cmd *cobra.Command) string {
	name := cmd.Use
	if i := strings.IndexByte(name, ' '); i >= 0 {
		name = name[:i]
	}
	return name
}

// RedactHomeDir collapses environment-rooted prefixes so path strings
// can be safely surfaced through `config policy show` and resolver
// error messages without leaking the user's filesystem layout to AI
// agents / CI logs.
//
// It folds, in priority order:
//  1. core.GetBaseConfigDir() (typically ~/.lark-cli, or a custom
//     directory under LARKSUITE_CLI_CONFIG_DIR — e.g.
//     "/private/tmp/sandbox/.lark-cli" in a sandboxed run) → "<config>"
//  2. The user's home directory → "~"
//
// (1) runs first so a `LARKSUITE_CLI_CONFIG_DIR` pointing outside `$HOME`
// still produces a stable, non-identifying label. When neither prefix
// matches, the input is returned unchanged — those cases don't leak
// anything that wasn't already passed in by the caller.
func RedactHomeDir(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	if rel, ok := foldPrefix(abs, core.GetBaseConfigDir()); ok {
		if rel == "" {
			return "<config>"
		}
		return "<config>/" + rel
	}

	home, err := vfs.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, ok := foldPrefix(abs, home); ok {
		if rel == "" {
			return "~"
		}
		return "~/" + rel
	}
	return path
}

// foldPrefix reports whether abs lives at or beneath prefix; on hit it
// returns the slash-form relative tail (empty when abs == prefix).
func foldPrefix(abs, prefix string) (string, bool) {
	if prefix == "" {
		return "", false
	}
	absPrefix, err := filepath.Abs(prefix)
	if err != nil {
		absPrefix = prefix
	}
	rel, err := filepath.Rel(absPrefix, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	return filepath.ToSlash(rel), true
}
