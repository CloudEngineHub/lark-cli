// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package pruning

import (
	"github.com/spf13/cobra"

	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/policydecision"
)

// Apply walks the command tree and installs denyStubs for every path in
// deniedByPath whose Denial.Layer == "pruning". It is the user-layer
// counterpart to applyStrictModeDenials in cmd/prune.go; both consume the
// same deniedByPath map produced by the bootstrap pipeline, neither
// re-evaluates rules.
//
// Three things must happen for every denied command (hard-constraints 1-4
// in the tech doc):
//
//  1. cmd.Hidden = true                -- removes from help / completion
//  2. cmd.DisableFlagParsing = true    -- denial-wins invariant; otherwise
//                                         cobra would intercept the call
//                                         with "missing required flag"
//                                         before we can return our error
//  3. cmd.RunE = denyStub(denial)      -- returns *output.ExitError so
//                                         cmd/root.go's envelope writer
//                                         emits structured JSON (with
//                                         error.type = denial.Layer and
//                                         detail.reason_code = ReasonCode);
//                                         the wrapped error chain still
//                                         exposes *platform.CommandDeniedError
//                                         via errors.As for in-process
//                                         consumers
//
// Apply must be called once during the Bootstrap pipeline BEFORE
// cobra.Execute. It mutates the command tree in place and is not safe to
// call concurrently with command dispatch. Returns the number of commands
// modified.
func Apply(root *cobra.Command, deniedByPath map[string]policydecision.Denial) int {
	if root == nil || len(deniedByPath) == 0 {
		return 0
	}

	count := 0
	walkTree(root, func(c *cobra.Command) {
		// Never install a denyStub on the binary root itself. Even if the
		// aggregation pass somehow marked it (e.g. all-children-denied at
		// the top), the binary entry point must remain dispatchable so
		// cobra's own help / completion paths still work.
		if !c.HasParent() {
			return
		}
		path := CanonicalPath(c)
		if path == "" {
			return
		}
		d, ok := deniedByPath[path]
		if !ok || d.Layer != policydecision.LayerPruning {
			return
		}
		installDenyStub(c, path, d)
		count++
	})
	return count
}

// AnnotationDenialLayer is the cobra annotation key written by
// installDenyStub to signal "this command is denied" to layers above
// the pruning package (specifically internal/hook reads it to populate
// Invocation.DeniedByPolicy without importing pruning, avoiding an
// import cycle).
const AnnotationDenialLayer = "lark:pruning_denied_layer"

// AnnotationDenialSource records the matching PolicySource so the hook
// layer can populate Invocation.DenialPolicySource() with the right
// value.
const AnnotationDenialSource = "lark:pruning_denied_source"

// installDenyStub mutates a cobra.Command in place. Unlike cmd/prune.go
// which does RemoveCommand+AddCommand (changing the pointer), we modify
// the existing node so any external reference (snapshots, alias targets)
// continues to point at the same cmd.
//
// Help fields (cmd.Short / cmd.Long / cmd.Flags()) are deliberately
// preserved so `--help` on a denied command still describes what the
// command was intended to do.
//
// Two cobra Annotations are set as a denial signal that internal/hook
// reads (without taking a dependency on this package):
//
//   - AnnotationDenialLayer  -> "pruning" or "strict_mode"
//   - AnnotationDenialSource -> the PolicySource ("yaml", "plugin:foo", ...)
func installDenyStub(cmd *cobra.Command, path string, d policydecision.Denial) {
	// strict-mode wins over user-layer pruning. If the command was
	// already replaced by a strict-mode stub (cmd/prune.go::strictModeStubFrom
	// writes layer=strict_mode), do NOT overwrite -- the user-layer
	// rule cannot relax or relabel a credential-hard boundary.
	//
	// Behaviour without this guard (pre-fix): a user yaml rule matching
	// a strict-mode stub's path would replace the RunE with the pruning
	// denyStub, hiding the original strict-mode error message AND
	// re-labelling error.type from "strict_mode" to "pruning".
	if cmd.Annotations != nil &&
		cmd.Annotations[AnnotationDenialLayer] == policydecision.LayerStrictMode {
		return
	}
	cmd.Hidden = true
	cmd.DisableFlagParsing = true

	// Bypass cobra's pre-RunE gates that would otherwise short-circuit
	// before the wrapped RunE (= where observers + denial guard live):
	//
	//   1. Args validator: original commands often declare cobra.NoArgs
	//      or a custom Args function. With DisableFlagParsing=true,
	//      `--doc xxx` looks like positional args; cobra.ValidateArgs
	//      fires BEFORE PersistentPreRunE / PreRunE / RunE and would
	//      surface a Cobra usage error instead of our pruning envelope.
	//      ArbitraryArgs accepts everything.
	//
	//   2. Parent's PersistentPreRunE: cobra's "first PersistentPreRunE
	//      wins" walks UP from the leaf. cmd/auth/auth.go declares a
	//      PersistentPreRunE that returns external_provider when env
	//      credentials are set; without our leaf-level override, that
	//      fires before pruning's RunE and the caller sees the wrong
	//      envelope. We set a no-op leaf PersistentPreRunE that just
	//      silences usage and returns nil, so dispatch proceeds to the
	//      wrapped RunE (which produces the real pruning envelope and
	//      lets Before/After observers fire).
	cmd.Args = cobra.ArbitraryArgs
	cmd.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		c.SilenceUsage = true
		return nil
	}
	cmd.PersistentPreRun = nil
	cmd.PreRunE = nil
	cmd.PreRun = nil

	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[AnnotationDenialLayer] = d.Layer
	cmd.Annotations[AnnotationDenialSource] = d.PolicySource

	denial := d // capture by value for the closure
	cmd.RunE = func(c *cobra.Command, args []string) error {
		cd := &platform.CommandDeniedError{
			Path:         path,
			Layer:        denial.Layer,
			PolicySource: denial.PolicySource,
			RuleName:     denial.RuleName,
			ReasonCode:   denial.ReasonCode,
			Reason:       denial.Reason,
		}
		// Wrap in *output.ExitError so the cmd/root.go envelope writer
		// emits a JSON envelope. error.type uses denial.Layer ("pruning"
		// / "strict_mode") to match the design-doc contract. The detail
		// map carries the closed-enum reason_code plus the structured
		// fields agents read.
		return &output.ExitError{
			Code: output.ExitValidation,
			Detail: &output.ErrDetail{
				Type:    denial.Layer,
				Message: cd.Error(),
				Detail: map[string]any{
					"path":          cd.Path,
					"layer":         cd.Layer,
					"policy_source": cd.PolicySource,
					"rule_name":     cd.RuleName,
					"reason_code":   cd.ReasonCode,
					"reason":        cd.Reason,
				},
			},
			Err: cd, // preserved for errors.As-style consumers
		}
	}
	// Clear any pre-existing Run hook: cobra prefers RunE when both are
	// set, but leaving a stale Run around is a foot-gun for future
	// maintainers.
	cmd.Run = nil
}
