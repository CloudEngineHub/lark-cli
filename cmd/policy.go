// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/hook"
	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/platformhost"
	"github.com/larksuite/cli/internal/plugininventory"
	"github.com/larksuite/cli/internal/pruning"
	"github.com/larksuite/cli/internal/vfs"
)

// userPolicyFileName is the conventional filename for the user-layer Rule.
// Lives under ~/.lark-cli/ to match the rest of the CLI's user-state
// directory.
const userPolicyFileName = "policy.yml"

// applyUserPolicyPruning resolves the user-layer Rule from plugin
// contributions and/or ~/.lark-cli/policy.yml and installs denyStubs
// for commands it rejects.
//
// Missing yaml is not an error -- the CLI runs with no user-layer
// restriction. A malformed Rule (bad MaxRisk enum, malformed glob, etc.)
// surfaces via the returned error; the caller decides how to handle it.
//
// pluginRules carries Plugin.Restrict() contributions collected from
// the platformhost InstallAll phase; nil/empty is fine.
func applyUserPolicyPruning(rootCmd *cobra.Command, pluginRules []pruning.PluginRule) error {
	yamlPath, err := userPolicyPath()
	if err != nil {
		// No user home dir means we cannot locate the policy. Treat
		// the same as "file missing": no pruning, no error. This keeps
		// non-interactive CI environments (no HOME set) running.
		yamlPath = ""
	}

	rule, source, err := pruning.Resolve(pluginRules, yamlPath)
	if err != nil {
		return err
	}
	if rule == nil {
		pruning.SetActive(&pruning.ActivePolicy{
			Source:   source,
			YAMLPath: yamlPath,
		})
		return nil
	}

	engine := pruning.New(rule)
	decisions := engine.EvaluateAll(rootCmd)
	denied := pruning.BuildDeniedByPath(rootCmd, decisions, source, rule.Name)
	pruning.Apply(rootCmd, denied)

	// Record the active policy so `config policy show` can read it.
	pruning.SetActive(&pruning.ActivePolicy{
		Rule:        rule,
		Source:      source,
		YAMLPath:    yamlPath,
		DeniedPaths: len(denied),
	})
	return nil
}

// installPluginsAndHooks runs the platformhost.InstallAll phase on the
// globally-registered plugins, returning the Plugin.Restrict
// contributions for pruning and the populated hook.Registry for the
// runtime wrapper. Errors from FailClosed plugins propagate; FailOpen
// failures are warned to errOut and the loop continues.
func installPluginsAndHooks(errOut io.Writer) (*platformhost.InstallResult, error) {
	plugins := platform.RegisteredPlugins()
	if len(plugins) == 0 {
		return &platformhost.InstallResult{Registry: nil}, nil
	}
	return platformhost.InstallAll(plugins, errOut)
}

// recordInventory builds and stores the plugin inventory snapshot for
// diagnostic commands (config plugins show) to read at runtime. Called
// once from build.go after applyUserPolicyPruning + wireHooks succeed.
func recordInventory(installResult *platformhost.InstallResult) {
	if installResult == nil {
		plugininventory.SetActive(nil)
		return
	}
	pluginSrcs := make([]plugininventory.PluginSource, 0, len(installResult.Plugins))
	for _, p := range installResult.Plugins {
		pluginSrcs = append(pluginSrcs, plugininventory.PluginSource{
			Name:         p.Name,
			Version:      p.Version,
			Capabilities: p.Capabilities,
		})
	}
	ruleSrcs := make([]plugininventory.RuleSource, 0, len(installResult.PluginRules))
	for _, r := range installResult.PluginRules {
		if r.Rule == nil {
			continue
		}
		ruleSrcs = append(ruleSrcs, plugininventory.RuleSource{
			PluginName: r.PluginName,
			Allow:      r.Rule.Allow,
			Deny:       r.Rule.Deny,
			MaxRisk:    r.Rule.MaxRisk,
			Identities: r.Rule.Identities,
			RuleName:   r.Rule.Name,
			Desc:       r.Rule.Description,
		})
	}
	plugininventory.SetActive(plugininventory.Build(pluginSrcs, installResult.Registry, ruleSrcs))
}

// wireHooks installs Observer/Wrapper hooks onto every runnable command
// and emits the Startup lifecycle event. The registry may be nil when
// no plugin contributed any hook -- the function short-circuits in
// that case to avoid useless RunE wrapping.
func wireHooks(ctx context.Context, rootCmd *cobra.Command, reg *hook.Registry) error {
	if reg == nil {
		return nil
	}
	hook.Install(rootCmd, reg, cobraCommandViewSource{})
	return hook.Emit(ctx, reg, platform.Startup, nil)
}

// installFatalGuard wires a fail-closed guard at every cobra dispatch
// path on rootCmd. Used by the three abort-side fatal paths:
//
//   - FailClosed plugin install failure  (installPluginInstallErrorGuard)
//   - Plugin Restrict conflict           (installPluginConflictGuard)
//   - Startup lifecycle handler failure  (installPluginLifecycleErrorGuard)
//
// **Why we walk the tree rather than set PersistentPreRunE on root**:
// cobra's PersistentPreRunE has "first PersistentPreRunE wins"
// semantics -- the lookup starts at the invoked command and walks UP,
// stopping at the first non-nil PersistentPreRunE. Subcommands that
// declare their own PersistentPreRunE (cmd/auth/auth.go and
// cmd/config/config.go both do) would shadow root's, letting a
// fail-closed condition silently bypass via `lark-cli auth foo`.
//
// The fix: replace the RunE of every runnable command with one that
// returns makeErr(). Subcommands cannot bypass because the dispatch
// lands directly on their RunE, which now carries the guard.
//
// makeErr is called for every guarded dispatch; it must return a fresh
// *output.ExitError each time (the envelope writer mutates a few fields
// as it serialises).
func installFatalGuard(rootCmd *cobra.Command, makeErr func() *output.ExitError) {
	// Two cobra subcommands are injected lazily at Execute() time and
	// would otherwise slip past walkGuard. We pre-register both so
	// walkGuard catches them.
	//
	//   - "completion" (user-visible): InitDefaultCompletionCmd
	//   - "__complete" (internal shell-completion RPC): no public
	//     constructor; we add our own stub with the same name. cobra's
	//     internal initCompleteCmd checks for an existing "__complete"
	//     and skips registration if found, so our stub stays in place.
	//     (Cobra dispatches the "__completeNoDesc" alias through the
	//     same RunE, so guarding "__complete" covers both.)
	rootCmd.InitDefaultCompletionCmd()
	alreadyPresent := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "__complete" {
			alreadyPresent = true
			break
		}
	}
	if !alreadyPresent {
		rootCmd.AddCommand(&cobra.Command{
			Use:    "__complete",
			Hidden: true,
			RunE:   func(*cobra.Command, []string) error { return makeErr() },
		})
	}

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		return makeErr()
	}
	rootCmd.PersistentPreRun = nil
	walkGuard(rootCmd, makeErr)
}

// installPluginInstallErrorGuard surfaces a FailClosed plugin install
// failure as a structured plugin_install envelope before any command
// runs.
func installPluginInstallErrorGuard(rootCmd *cobra.Command, installErr error) {
	makeErr := func() *output.ExitError {
		var pi *platformhost.PluginInstallError
		if errors.As(installErr, &pi) {
			return &output.ExitError{
				Code: output.ExitValidation,
				Detail: &output.ErrDetail{
					Type:    "plugin_install",
					Message: pi.Error(),
					Detail: map[string]any{
						"plugin":      pi.PluginName,
						"reason_code": pi.ReasonCode,
						"reason":      pi.Reason,
					},
				},
				Err: installErr,
			}
		}
		return &output.ExitError{
			Code: output.ExitValidation,
			Detail: &output.ErrDetail{
				Type:    "plugin_install",
				Message: installErr.Error(),
				Detail: map[string]any{
					"reason_code": platformhost.ReasonInstallFailed,
				},
			},
			Err: installErr,
		}
	}
	installFatalGuard(rootCmd, makeErr)
}

// installPluginConflictGuard surfaces a Plugin.Restrict() configuration
// error (single plugin invalid Rule or multiple plugins each contributing
// Restrict). The tech doc separates the envelope type:
//
//   - "plugin_install" with reason_code "invalid_rule"           - single bad rule
//   - "plugin_conflict" with reason_code "multiple_restrict_plugins" - multi
//
// Either way the CLI must NOT silently continue with a broken policy.
func installPluginConflictGuard(rootCmd *cobra.Command, err error) {
	makeErr := func() *output.ExitError {
		envelopeType := "plugin_install"
		reasonCode := platformhost.ReasonInvalidRule
		if errors.Is(err, pruning.ErrMultipleRestricts) {
			envelopeType = "plugin_conflict"
			reasonCode = platformhost.ReasonMultipleRestricts
		}
		return &output.ExitError{
			Code: output.ExitValidation,
			Detail: &output.ErrDetail{
				Type:    envelopeType,
				Message: err.Error(),
				Detail: map[string]any{
					"reason_code": reasonCode,
				},
			},
			Err: err,
		}
	}
	installFatalGuard(rootCmd, makeErr)
}

// installPluginLifecycleErrorGuard surfaces a Startup lifecycle handler
// failure as a plugin_lifecycle envelope. The reason_code splits
// returned-error vs panic so consumers (audit / on-call) can tell the
// two failure modes apart.
//
// Per tech-doc table line 523: type=plugin_lifecycle, reason_code in
// {lifecycle_failed, lifecycle_panic}.
func installPluginLifecycleErrorGuard(rootCmd *cobra.Command, err error) {
	makeErr := func() *output.ExitError {
		reasonCode := "lifecycle_failed"
		detail := map[string]any{
			"reason_code": reasonCode,
		}
		var le *hook.LifecycleError
		if errors.As(err, &le) {
			if le.Panic {
				reasonCode = "lifecycle_panic"
			}
			detail = map[string]any{
				"reason_code": reasonCode,
				"hook_name":   le.HookName,
				"event":       "startup",
			}
		}
		return &output.ExitError{
			Code: output.ExitValidation,
			Detail: &output.ErrDetail{
				Type:    "plugin_lifecycle",
				Message: err.Error(),
				Detail:  detail,
			},
			Err: err,
		}
	}
	installFatalGuard(rootCmd, makeErr)
}

// walkGuard recurses through cmd's subtree and installs the guard at
// EVERY level cobra might dispatch to. The cobra execution order is:
//
//  1. PersistentPreRunE (looked up from leaf, walking up; "first wins")
//  2. PreRunE
//  3. RunE
//  4. PostRunE
//  5. PersistentPostRunE
//
// A subcommand that declares its own PersistentPreRunE (cmd/auth and
// cmd/config both do) would not only shadow root's PersistentPreRunE
// -- if that PreRunE itself returns an error (e.g. auth's
// external_provider check), the user sees THAT error instead of
// our plugin_install envelope, even if RunE was guarded.
//
// To close every dispatch hole we replace:
//   - every command's PersistentPreRunE (including non-runnable groups)
//   - every runnable command's PreRunE and RunE
//
// This way the very first non-nil step in cobra's chain is always our
// guard, regardless of which leaf the user invoked.
func walkGuard(cmd *cobra.Command, makeErr func() *output.ExitError) {
	if cmd == nil {
		return
	}
	// PersistentPreRunE is the first step cobra runs (after Args /
	// flag validation -- see below). Set it on every command (root
	// included) so cobra's "first wins" walk-up always finds OUR
	// PersistentPreRunE before hitting any subcommand's pre-existing
	// one.
	cmd.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		c.SilenceUsage = true
		return makeErr()
	}
	cmd.PersistentPreRun = nil

	// **Cobra dispatch order before PersistentPreRunE:**
	//   1. ValidateArgs(cmd.Args)            -- can return arg error
	//   2. ParsePersistentFlags / ParseFlags -- can return flag error
	//   3. Find legacyArgs check for unknown-command at root
	//   4. PersistentPreRunE / PreRunE / RunE
	//   5. Non-runnable groups fall through to help (PreRunE skipped)
	//
	// We neutralise each step:
	//   - Args = ArbitraryArgs     -> ValidateArgs no-op. **Not nil**:
	//                                 cobra falls back to legacyArgs
	//                                 when Args==nil, which returns an
	//                                 unknown-command error during Find
	//                                 BEFORE PersistentPreRunE runs.
	//                                 ArbitraryArgs explicitly accepts
	//                                 everything, suppressing that path.
	//   - DisableFlagParsing       -> ParseFlags skipped (and legacy
	//                                 "unknown flag" suppressed)
	//   - PreRunE / RunE on EVERY  -> Even non-runnable groups now run
	//     command (not just leaves)   the guard instead of showing help
	//
	// Setting RunE on a parent group flips Runnable() to true, so
	// cobra dispatches to it (and our guard fires) rather than calling
	// the help command on a "help-only" group.
	cmd.Args = cobra.ArbitraryArgs
	cmd.DisableFlagParsing = true
	cmd.PreRunE = func(c *cobra.Command, args []string) error {
		c.SilenceUsage = true
		return makeErr()
	}
	cmd.PreRun = nil
	cmd.RunE = func(*cobra.Command, []string) error { return makeErr() }
	cmd.Run = nil
	for _, c := range cmd.Commands() {
		walkGuard(c, makeErr)
	}
}

// cobraCommandViewSource is the default CommandViewSource: it builds a
// CommandView directly from a *cobra.Command on demand. A future PR
// will snapshot views at registration time (constraint #1 fully) so
// the view survives strict-mode's RemoveCommand+AddCommand replacement
// of the underlying *cobra.Command pointer. For now this is acceptable
// because user-layer pruning preserves the pointer (only strict-mode
// swaps it), and strict-mode-pruned commands are already unreachable
// by the hook chain.
type cobraCommandViewSource struct{}

func (cobraCommandViewSource) View(cmd *cobra.Command) platform.CommandView {
	return cobraCommandView{cmd: cmd}
}

// cobraCommandView adapts *cobra.Command to the CommandView interface.
type cobraCommandView struct {
	cmd *cobra.Command
}

func (v cobraCommandView) Path() string {
	return pruning.CanonicalPath(v.cmd)
}

func (v cobraCommandView) Domain() string {
	// cmdmeta inheritance is implemented in internal/cmdmeta; we
	// re-read annotations directly here to keep the import surface
	// small. Future PR may pull cmdmeta into the View.
	for c := v.cmd; c != nil; c = c.Parent() {
		if c.Annotations == nil {
			continue
		}
		if v, ok := c.Annotations["cmdmeta.domain"]; ok && v != "" {
			return v
		}
	}
	return ""
}

func (v cobraCommandView) Risk() (string, bool) {
	for c := v.cmd; c != nil; c = c.Parent() {
		if c.Annotations == nil {
			continue
		}
		if r, ok := c.Annotations["risk_level"]; ok && r != "" {
			return r, true
		}
	}
	return "", false
}

func (v cobraCommandView) Identities() []string {
	for c := v.cmd; c != nil; c = c.Parent() {
		if c.Annotations == nil {
			continue
		}
		if raw, ok := c.Annotations["lark:supportedIdentities"]; ok && raw != "" {
			return splitCSV(raw)
		}
	}
	return nil
}

func (v cobraCommandView) Annotation(key string) (string, bool) {
	if v.cmd.Annotations == nil {
		return "", false
	}
	s, ok := v.cmd.Annotations[key]
	return s, ok
}

// splitCSV is a tiny csv-without-quotes helper. CommandView is on the
// hot path (one lookup per command invocation) and we want to avoid
// pulling strings.Split's allocation cost; the lark:supportedIdentities
// annotation is always plain "user" / "bot" / "user,bot" without
// escaping.
func splitCSV(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// userPolicyPath returns the absolute path of ~/.lark-cli/policy.yml,
// or an error if the user's home directory cannot be determined.
func userPolicyPath() (string, error) {
	home, err := vfs.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".lark-cli", userPolicyFileName), nil
}

// warnPolicyError writes a one-line stderr warning when the user policy
// fails to load. V1 yaml errors are fail-OPEN -- the CLI keeps running
// without pruning so the user can fix the typo. Plugin-supplied rules
// (Hook surface, future) will be fail-CLOSED instead because integrators
// take a code-level responsibility for them.
func warnPolicyError(errOut io.Writer, err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(errOut, "warning: user policy not applied: %v\n", err)
}
