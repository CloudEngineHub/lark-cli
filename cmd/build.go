// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"io"

	"github.com/larksuite/cli/cmd/api"
	"github.com/larksuite/cli/cmd/auth"
	"github.com/larksuite/cli/cmd/completion"
	cmdconfig "github.com/larksuite/cli/cmd/config"
	"github.com/larksuite/cli/cmd/doctor"
	cmdevent "github.com/larksuite/cli/cmd/event"
	"github.com/larksuite/cli/cmd/profile"
	"github.com/larksuite/cli/cmd/schema"
	"github.com/larksuite/cli/cmd/service"
	cmdupdate "github.com/larksuite/cli/cmd/update"
	_ "github.com/larksuite/cli/events"
	"github.com/larksuite/cli/internal/build"
	"github.com/larksuite/cli/internal/cmdpolicy"
	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/hook"
	"github.com/larksuite/cli/internal/keychain"
	"github.com/larksuite/cli/shortcuts"
	"github.com/spf13/cobra"
)

// BuildOption configures optional aspects of the command tree construction.
type BuildOption func(*buildConfig)

type buildConfig struct {
	streams  *cmdutil.IOStreams
	keychain keychain.KeychainAccess
	globals  GlobalOptions
}

// WithIO sets the IO streams for the CLI by wrapping raw reader/writers.
// Terminal detection is delegated to cmdutil.NewIOStreams.
func WithIO(in io.Reader, out, errOut io.Writer) BuildOption {
	return func(c *buildConfig) {
		c.streams = cmdutil.NewIOStreams(in, out, errOut)
	}
}

// WithKeychain sets the secret storage backend. If not provided, the platform keychain is used.
func WithKeychain(kc keychain.KeychainAccess) BuildOption {
	return func(c *buildConfig) {
		c.keychain = kc
	}
}

// HideProfile sets the visibility policy for the root-level --profile flag.
// When hide is true the flag stays registered (so existing invocations still
// parse) but is omitted from help and shell completion. Typically called as
// HideProfile(isSingleAppMode()).
func HideProfile(hide bool) BuildOption {
	return func(c *buildConfig) {
		c.globals.HideProfile = hide
	}
}

// Build constructs the full command tree without executing.
// Returns only the cobra.Command; Factory and hook Registry are internal.
// Use Execute for the standard production entry point.
func Build(ctx context.Context, inv cmdutil.InvocationContext, opts ...BuildOption) *cobra.Command {
	_, rootCmd, _ := buildInternal(ctx, inv, opts...)
	return rootCmd
}

// buildInternal is a pure assembly function: it wires the command tree from
// inv and BuildOptions alone. Any state-dependent decision (disk, network,
// env) belongs in the caller and must be threaded in via BuildOption.
//
// Returns (factory, rootCmd, registry). The registry is nil when plugin
// install failed (FailClosed guard installed) or when no plugin produced
// hooks; callers that wire Shutdown emit must nil-check before calling
// hook.Emit.
func buildInternal(ctx context.Context, inv cmdutil.InvocationContext, opts ...BuildOption) (*cmdutil.Factory, *cobra.Command, *hook.Registry) {
	// cfg.globals.Profile is left zero here; it's bound to the --profile
	// flag in RegisterGlobalFlags and filled by cobra's parse step.
	cfg := &buildConfig{}
	for _, o := range opts {
		if o != nil {
			o(cfg)
		}
	}
	// Default streams when WithIO is not supplied so the root command's
	// SetIn/Out/Err calls below don't deref nil. NewDefault also normalizes
	// partial streams internally; keep both in sync so cfg.streams reflects
	// the same values the Factory ends up using.
	if cfg.streams == nil {
		cfg.streams = cmdutil.SystemIO()
	}

	f := cmdutil.NewDefault(cfg.streams, inv)
	if cfg.keychain != nil {
		f.Keychain = cfg.keychain
	}
	rootCmd := &cobra.Command{
		Use:     "lark-cli",
		Short:   "Lark/Feishu CLI — OAuth authorization, UAT management, API calls",
		Long:    rootLong,
		Version: build.Version,
	}

	rootCmd.SetContext(ctx)
	rootCmd.SetIn(cfg.streams.In)
	rootCmd.SetOut(cfg.streams.Out)
	rootCmd.SetErr(cfg.streams.ErrOut)

	installTipsHelpFunc(rootCmd)
	rootCmd.SilenceErrors = true

	RegisterGlobalFlags(rootCmd.PersistentFlags(), &cfg.globals)
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		cmd.SilenceUsage = true
		f.CurrentCommand = cmd
	}

	rootCmd.AddCommand(cmdconfig.NewCmdConfig(f))
	rootCmd.AddCommand(auth.NewCmdAuth(f))
	rootCmd.AddCommand(profile.NewCmdProfile(f))
	rootCmd.AddCommand(doctor.NewCmdDoctor(f))
	rootCmd.AddCommand(api.NewCmdApiWithContext(ctx, f, nil))
	rootCmd.AddCommand(schema.NewCmdSchema(f, nil))
	rootCmd.AddCommand(completion.NewCmdCompletion(f))
	rootCmd.AddCommand(cmdupdate.NewCmdUpdate(f))
	rootCmd.AddCommand(cmdevent.NewCmdEvents(f))
	service.RegisterServiceCommandsWithContext(ctx, rootCmd, f)
	shortcuts.RegisterShortcutsWithContext(ctx, rootCmd, f)

	installUnknownSubcommandGuard(rootCmd)

	// Prune commands incompatible with strict mode.
	if mode := f.ResolveStrictMode(ctx); mode.IsActive() {
		pruneForStrictMode(rootCmd, mode)
	}

	// Run the platform host: install registered plugins, collecting
	// their Restrict() contributions and their hooks. FailClosed
	// failures (and untrusted-config failures like restricts_mismatch)
	// are abort-worthy: InstallAll returns an error in those cases.
	// We honour that by installing a PersistentPreRunE that emits
	// the structured envelope at command-dispatch time -- buildInternal
	// itself cannot return an error without breaking its assembly
	// contract, but cobra runs PersistentPreRunE before any RunE so
	// the user sees the error on their very next invocation.
	installResult, installErr := installPluginsAndHooks(cfg.streams.ErrOut)
	if installErr != nil {
		installPluginInstallErrorGuard(rootCmd, installErr)
		// Stop wiring more state from a failed install -- the rest of
		// the function would only matter if the CLI is allowed to
		// proceed normally, which it isn't.
		return f, rootCmd, nil
	}
	var pluginRules []cmdpolicy.PluginRule
	var registry *hook.Registry
	if installResult != nil {
		pluginRules = installResult.PluginRules
		registry = installResult.Registry
	}

	// Apply user-layer command pruning: yaml + Plugin.Restrict.
	//
	// **Error policy splits by source**:
	//   - Plugin path (any pluginRules contributed): a validation or
	//     conflict error is a HARD failure -- the plugin author asked
	//     for a security policy, silently dropping it would leave the
	//     CLI more permissive than intended. Abort via the conflict
	//     guard so every command surfaces the structured envelope.
	//   - yaml-only path: stays fail-OPEN with a warning. A user typo
	//     in policy.yml must not lock them out of every command.
	if err := applyUserPolicyPruning(rootCmd, pluginRules); err != nil {
		if len(pluginRules) > 0 {
			installPluginConflictGuard(rootCmd, err)
			return f, rootCmd, nil
		}
		warnPolicyError(cfg.streams.ErrOut, err)
	}

	// Install hooks onto the (now-pruned) command tree and emit the
	// Startup lifecycle event so Plugin.On(Startup) handlers can run.
	//
	// Startup handler error or panic is a HARD failure: a plugin's
	// Startup logic is part of its install contract, and silently
	// continuing would mean the plugin's invariants do not hold while
	// the rest of its hooks (Wrap / Observe) still fire. Install the
	// plugin_lifecycle guard and short-circuit so every subsequent
	// dispatch surfaces the envelope.
	if registry != nil {
		if err := wireHooks(ctx, rootCmd, registry); err != nil {
			installPluginLifecycleErrorGuard(rootCmd, err)
			return f, rootCmd, nil
		}
	}

	// Snapshot the plugin inventory so `config plugins show` can answer
	// "what plugins / hooks / rules are currently in effect" without
	// re-calling plugin methods at display time.
	recordInventory(installResult)

	return f, rootCmd, registry
}
