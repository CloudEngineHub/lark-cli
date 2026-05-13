// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"
	"testing"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/policydecision"
	"github.com/larksuite/cli/internal/pruning"
	"github.com/spf13/cobra"
)

func newTestTree() *cobra.Command {
	root := &cobra.Command{Use: "root"}

	svc := &cobra.Command{Use: "im"}
	root.AddCommand(svc)

	noop := func(*cobra.Command, []string) error { return nil }

	userOnly := &cobra.Command{Use: "+search", Short: "user only", RunE: noop}
	cmdutil.SetSupportedIdentities(userOnly, []string{"user"})
	svc.AddCommand(userOnly)

	botOnly := &cobra.Command{Use: "+subscribe", Short: "bot only", RunE: noop}
	cmdutil.SetSupportedIdentities(botOnly, []string{"bot"})
	svc.AddCommand(botOnly)

	dual := &cobra.Command{Use: "+send", Short: "dual", RunE: noop}
	cmdutil.SetSupportedIdentities(dual, []string{"user", "bot"})
	svc.AddCommand(dual)

	noAnnotation := &cobra.Command{Use: "+legacy", Short: "no annotation", RunE: noop}
	svc.AddCommand(noAnnotation)

	res := &cobra.Command{Use: "messages"}
	svc.AddCommand(res)
	userMethod := &cobra.Command{Use: "search", RunE: func(*cobra.Command, []string) error { return nil }}
	cmdutil.SetSupportedIdentities(userMethod, []string{"user"})
	res.AddCommand(userMethod)

	auth := &cobra.Command{Use: "auth"}
	root.AddCommand(auth)
	login := &cobra.Command{Use: "login", RunE: noop}
	cmdutil.SetSupportedIdentities(login, []string{"user"})
	auth.AddCommand(login)

	return root
}

func findCmd(root *cobra.Command, names ...string) *cobra.Command {
	cmd := root
	for _, name := range names {
		found := false
		for _, c := range cmd.Commands() {
			if c.Name() == name {
				cmd = c
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cmd
}

func TestPruneForStrictMode_Bot(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeBot)

	if cmd := findCmd(root, "im", "+search"); cmd == nil || !cmd.Hidden {
		t.Error("+search (user-only) should be replaced by a hidden stub in bot mode")
	}
	if findCmd(root, "im", "+subscribe") == nil {
		t.Error("+subscribe (bot-only) should be kept in bot mode")
	}
	if findCmd(root, "im", "+send") == nil {
		t.Error("+send (dual) should be kept in bot mode")
	}
	if findCmd(root, "im", "+legacy") == nil {
		t.Error("+legacy (no annotation) should be kept")
	}
	if cmd := findCmd(root, "im", "messages", "search"); cmd == nil || !cmd.Hidden {
		t.Error("search (user-only method) should be replaced by a hidden stub in bot mode")
	}
	if cmd := findCmd(root, "auth", "login"); cmd == nil || !cmd.Hidden {
		t.Error("auth login should be replaced by a hidden stub in bot mode")
	}
}

func TestPruneForStrictMode_User(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeUser)

	if findCmd(root, "im", "+search") == nil {
		t.Error("+search (user-only) should be kept in user mode")
	}
	if cmd := findCmd(root, "im", "+subscribe"); cmd == nil || !cmd.Hidden {
		t.Error("+subscribe (bot-only) should be replaced by a hidden stub in user mode")
	}
	if findCmd(root, "im", "+send") == nil {
		t.Error("+send (dual) should be kept in user mode")
	}
	if cmd := findCmd(root, "auth", "login"); cmd == nil || cmd.Hidden {
		t.Error("auth login should be kept in user mode")
	}
}

func TestPruneEmpty(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeBot)

	if cmd := findCmd(root, "im", "messages"); cmd == nil || !cmd.Hidden {
		t.Error("resource 'messages' should be kept hidden when only hidden stubs remain")
	}
}

func TestPruneEmpty_PreservesOriginallyHiddenGroup(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	hidden := &cobra.Command{Use: "hidden", Hidden: true}
	root.AddCommand(hidden)
	hidden.AddCommand(&cobra.Command{
		Use:  "visible",
		RunE: func(*cobra.Command, []string) error { return nil },
	})

	pruneEmpty(root)

	if !hidden.Hidden {
		t.Fatal("expected originally hidden group to remain hidden")
	}
}

func TestPruneForStrictMode_Bot_DirectUserShortcutReturnsStrictMode(t *testing.T) {
	root := newTestTree()
	root.SilenceErrors = true
	root.SilenceUsage = true
	pruneForStrictMode(root, core.StrictModeBot)
	root.SetArgs([]string{"im", "+search", "--query", "hello"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	if !strings.Contains(err.Error(), `strict mode is "bot"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPruneForStrictMode_Bot_DirectNestedUserMethodReturnsStrictMode(t *testing.T) {
	root := newTestTree()
	root.SilenceErrors = true
	root.SilenceUsage = true
	pruneForStrictMode(root, core.StrictModeBot)
	root.SetArgs([]string{"im", "messages", "search", "--query", "hello"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	if !strings.Contains(err.Error(), `strict mode is "bot"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPruneForStrictMode_Bot_DirectAuthLoginReturnsStrictMode(t *testing.T) {
	root := newTestTree()
	root.SilenceErrors = true
	root.SilenceUsage = true
	pruneForStrictMode(root, core.StrictModeBot)
	root.SetArgs([]string{"auth", "login", "--json", "--scope", "im:message.send_as_user"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	if !strings.Contains(err.Error(), `strict mode is "bot"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPruneForStrictMode_User_DirectBotShortcutReturnsStrictMode(t *testing.T) {
	root := newTestTree()
	root.SilenceErrors = true
	root.SilenceUsage = true
	pruneForStrictMode(root, core.StrictModeUser)
	root.SetArgs([]string{"im", "+subscribe", "--topic", "x"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected strict-mode error")
	}
	if !strings.Contains(err.Error(), `strict mode is "user"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Regression for codex C13: a strict-mode stub whose PARENT declares
// a PersistentPreRunE (e.g. cmd/auth/auth.go's external_provider
// check on env credentials) must surface the strict_mode envelope,
// not the parent's error. Cobra's "first PersistentPreRunE wins
// walking up from leaf" semantics will pick the parent's unless the
// stub itself carries its own.
//
// Fix: strictModeStubFrom installs a no-op PersistentPreRunE so cobra
// stops at the stub and proceeds to its RunE.
func TestStrictModeStub_BypassesParentPersistentPreRunE(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeBot)
	stub := findCmd(root, "auth", "login")
	if stub == nil {
		t.Fatal("auth/login stub should exist after StrictModeBot")
	}
	if stub.PersistentPreRunE == nil {
		t.Fatal("strict-mode stub must declare PersistentPreRunE on leaf")
	}
	if err := stub.PersistentPreRunE(stub, nil); err != nil {
		t.Errorf("strict-mode stub PersistentPreRunE should be no-op, got %v", err)
	}
}

// Regression for codex H13: strict-mode stub must accept arbitrary
// positional args. With DisableFlagParsing=true, a user passing
// `auth login --scope ...` looks like 4 positional args; the original
// cobra.Args validator would surface a usage error BEFORE strict-mode
// stub's RunE.
func TestStrictModeStub_BypassesArgsValidator(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeBot)
	stub := findCmd(root, "auth", "login")
	if stub == nil {
		t.Fatal("auth/login stub should exist after StrictModeBot")
	}
	if stub.Args == nil {
		t.Fatal("strict-mode stub must declare Args validator")
	}
	if err := stub.Args(stub, []string{"--scope", "im.message", "--profile", "default"}); err != nil {
		t.Errorf("strict-mode stub Args should accept flag-like args, got %v", err)
	}
}

// strictModeStubFrom must write the denial annotations so the hook
// layer's populateInvocationDenial recognises the command as denied
// and physically isolates the Wrap chain. Without this, a plugin
// Wrapper registered against platform.All() could intercept the stub
// and silently return nil, swallowing the strict-mode error.
func TestStrictModeStub_HasDenialAnnotation(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeBot)

	// im/+search is user-only -> replaced by a stub in StrictModeBot.
	stub := findCmd(root, "im", "+search")
	if stub == nil {
		t.Fatalf("expected im/+search stub to exist")
	}
	got := stub.Annotations[pruning.AnnotationDenialLayer]
	if got != policydecision.LayerStrictMode {
		t.Errorf("stub annotation %q = %q, want %q",
			pruning.AnnotationDenialLayer, got, policydecision.LayerStrictMode)
	}
	if src := stub.Annotations[pruning.AnnotationDenialSource]; src != "strict-mode" {
		t.Errorf("stub annotation %q = %q, want %q",
			pruning.AnnotationDenialSource, src, "strict-mode")
	}
}
