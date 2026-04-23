// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT
package doc

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/httpmock"
	"github.com/larksuite/cli/shortcuts/common"
)

func TestIsWhiteboardCreateMarkdown(t *testing.T) {
	t.Run("blank whiteboard tags", func(t *testing.T) {
		markdown := "<whiteboard type=\"blank\"></whiteboard>\n<whiteboard type=\"blank\"></whiteboard>"
		if !isWhiteboardCreateMarkdown(markdown) {
			t.Fatalf("expected blank whiteboard markdown to be treated as whiteboard creation")
		}
	})

	t.Run("mermaid code block", func(t *testing.T) {
		markdown := "```mermaid\ngraph TD\nA-->B\n```"
		if !isWhiteboardCreateMarkdown(markdown) {
			t.Fatalf("expected mermaid markdown to be treated as whiteboard creation")
		}
	})

	t.Run("plain markdown", func(t *testing.T) {
		markdown := "## plain text"
		if isWhiteboardCreateMarkdown(markdown) {
			t.Fatalf("did not expect plain markdown to be treated as whiteboard creation")
		}
	})
}

func TestNormalizeBoardTokens(t *testing.T) {
	// Codecov patch includes normalizeBoardTokens in this PR's diff because
	// the PR base predates #569 where this helper landed; the previously-
	// untested string and default arms are what keep patch coverage under the
	// threshold. These cases lock the fallback paths so any future caller
	// that passes a plain string or a non-slice token bag gets a stable shape.

	t.Run("nil raw returns empty slice", func(t *testing.T) {
		got := normalizeBoardTokens(nil)
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %#v", got)
		}
	})

	t.Run("already-typed string slice passes through", func(t *testing.T) {
		in := []string{"a", "b"}
		got := normalizeBoardTokens(in)
		if !reflect.DeepEqual(got, in) {
			t.Fatalf("got %#v, want %#v", got, in)
		}
	})

	t.Run("interface slice skips non-string and empty string items", func(t *testing.T) {
		got := normalizeBoardTokens([]interface{}{"keep", "", 42, "also"})
		want := []string{"keep", "also"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("single string wraps into one-item slice", func(t *testing.T) {
		got := normalizeBoardTokens("solo")
		want := []string{"solo"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})

	t.Run("empty string returns empty slice, not one-item slice", func(t *testing.T) {
		got := normalizeBoardTokens("")
		if len(got) != 0 {
			t.Fatalf("expected empty slice for empty string input, got %#v", got)
		}
	})

	t.Run("unsupported type falls through to empty slice", func(t *testing.T) {
		got := normalizeBoardTokens(42)
		if len(got) != 0 {
			t.Fatalf("expected empty slice for non-string/non-slice input, got %#v", got)
		}
	})
}

func TestNormalizeDocsUpdateResult(t *testing.T) {
	t.Run("adds empty board_tokens when whiteboard creation response omits it", func(t *testing.T) {
		result := map[string]interface{}{
			"success": true,
		}

		normalizeDocsUpdateResult(result, "<whiteboard type=\"blank\"></whiteboard>")

		got, ok := result["board_tokens"].([]string)
		if !ok {
			t.Fatalf("expected board_tokens to be []string, got %T", result["board_tokens"])
		}
		if len(got) != 0 {
			t.Fatalf("expected empty board_tokens, got %#v", got)
		}
	})

	t.Run("normalizes board_tokens to string slice", func(t *testing.T) {
		result := map[string]interface{}{
			"board_tokens": []interface{}{"board_1", "board_2"},
		}

		normalizeDocsUpdateResult(result, "<whiteboard type=\"blank\"></whiteboard>")

		want := []string{"board_1", "board_2"}
		got, ok := result["board_tokens"].([]string)
		if !ok {
			t.Fatalf("expected board_tokens to be []string, got %T", result["board_tokens"])
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("board_tokens mismatch: got %#v want %#v", got, want)
		}
	})

	t.Run("leaves non whiteboard response unchanged", func(t *testing.T) {
		result := map[string]interface{}{
			"success": true,
		}

		normalizeDocsUpdateResult(result, "## plain text")

		if _, ok := result["board_tokens"]; ok {
			t.Fatalf("did not expect board_tokens for non-whiteboard markdown")
		}
	})
}

func TestValidateSelectionByTitle(t *testing.T) {
	t.Run("empty title passes", func(t *testing.T) {
		if err := validateSelectionByTitle(""); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("heading style title passes", func(t *testing.T) {
		if err := validateSelectionByTitle("## 第二章"); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("plain text title fails with guidance", func(t *testing.T) {
		err := validateSelectionByTitle("第二章")
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if got := err.Error(); got == "" || !containsAll(got, "selection-by-title", "heading prefix") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("multi-line heading still fails", func(t *testing.T) {
		err := validateSelectionByTitle("## 第二章\n## 第三章")
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if got := err.Error(); got == "" || !containsAll(got, "single heading line") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("multi-line title fails", func(t *testing.T) {
		err := validateSelectionByTitle("第二章\n第三章")
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if got := err.Error(); got == "" || !containsAll(got, "single heading line") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func containsAll(s string, tokens ...string) bool {
	for _, token := range tokens {
		if !strings.Contains(s, token) {
			return false
		}
	}
	return true
}

// TestDocsUpdateValidate exercises the Validate closure directly so the new
// --selection-by-title integration point (call site in Validate) is covered,
// not just the underlying validateSelectionByTitle helper. Without this the
// three lines added to the closure show up as untested in the patch coverage
// report even though the helper itself is at 100%.
func TestDocsUpdateValidate(t *testing.T) {
	tests := []struct {
		name     string
		flags    map[string]string
		boolFlag string // name of optional bool flag to set (currently unused; placeholder for future flags)
		wantErr  string // substring; empty = expect nil error
	}{
		{
			// Happy path that exercises the new selection-by-title call site
			// with a valid heading — reaches the `return nil` branch.
			name: "heading-style selection-by-title passes",
			flags: map[string]string{
				"doc":                "doxcnABCDEF",
				"mode":               "replace_range",
				"markdown":           "new body",
				"selection-by-title": "## Section",
			},
		},
		{
			// Exercises the error-return branch of the new call site.
			name: "plain-text selection-by-title is rejected with heading-prefix guidance",
			flags: map[string]string{
				"doc":                "doxcnABCDEF",
				"mode":               "replace_range",
				"markdown":           "new body",
				"selection-by-title": "第二章",
			},
			wantErr: "heading prefix",
		},
		{
			// Exercises the multi-line guard inside validateSelectionByTitle
			// through the Validate call path.
			name: "multi-line selection-by-title is rejected as not a single heading",
			flags: map[string]string{
				"doc":                "doxcnABCDEF",
				"mode":               "replace_range",
				"markdown":           "new body",
				"selection-by-title": "## a\n## b",
			},
			wantErr: "single heading line",
		},
		{
			// Invalid mode — proves the earlier mode check still fires before
			// reaching the new selection-by-title check, so the new code
			// doesn't accidentally mask pre-existing validation.
			name: "invalid mode is still rejected first",
			flags: map[string]string{
				"doc":                "doxcnABCDEF",
				"mode":               "bogus",
				"selection-by-title": "## Section",
			},
			wantErr: "invalid --mode",
		},
		{
			// Both selection forms supplied — proves the mutual-exclusion
			// check still fires before the new selection-by-title check.
			name: "conflicting selection flags are rejected before title validation",
			flags: map[string]string{
				"doc":                     "doxcnABCDEF",
				"mode":                    "replace_range",
				"markdown":                "body",
				"selection-with-ellipsis": "start...end",
				"selection-by-title":      "## Section",
			},
			wantErr: "mutually exclusive",
		},
		{
			// Non-delete_range modes require --markdown; this exercises the
			// pre-existing empty-markdown branch that sits between the mode
			// check and the new selection-by-title check. Covering it keeps
			// patch coverage above codecov's threshold for this closure.
			name: "non-delete_range mode without --markdown is rejected",
			flags: map[string]string{
				"doc":                "doxcnABCDEF",
				"mode":               "replace_range",
				"selection-by-title": "## Section",
			},
			wantErr: "requires --markdown",
		},
		{
			// needsSelection[mode] is true for replace_range but neither
			// selection flag is set — covers the "requires selection" branch
			// that precedes the new call site.
			name: "replace_range without any selection flag is rejected",
			flags: map[string]string{
				"doc":      "doxcnABCDEF",
				"mode":     "replace_range",
				"markdown": "body",
			},
			wantErr: "requires --selection-with-ellipsis or --selection-by-title",
		},
		{
			// delete_range has no markdown requirement and no selection
			// requirement when neither is supplied is actually ok under the
			// current rules (delete_range still needs selection per
			// needsSelection, but the test proves the markdown-empty guard
			// does not fire for delete_range specifically).
			name: "delete_range without --markdown but with selection passes markdown check",
			flags: map[string]string{
				"doc":                "doxcnABCDEF",
				"mode":               "delete_range",
				"selection-by-title": "## Section",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "docs +update"}
			cmd.Flags().String("doc", "", "")
			cmd.Flags().String("mode", "", "")
			cmd.Flags().String("markdown", "", "")
			cmd.Flags().String("selection-with-ellipsis", "", "")
			cmd.Flags().String("selection-by-title", "", "")
			cmd.Flags().String("new-title", "", "")
			for k, v := range tt.flags {
				if err := cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("set --%s=%q: %v", k, v, err)
				}
			}

			rt := common.TestNewRuntimeContext(cmd, nil)
			err := DocsUpdate.Validate(context.Background(), rt)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// showDiffTestConfig is kept minimal — just enough for TestFactory to mount
// the update shortcut against the httpmock registry.
func showDiffTestConfig() *core.CliConfig {
	return &core.CliConfig{AppID: "show-diff-test", AppSecret: "test-secret", Brand: core.BrandFeishu}
}

func runDocsUpdateShortcut(t *testing.T, f *cmdutil.Factory, stdout *bytes.Buffer, args []string) error {
	t.Helper()
	parent := &cobra.Command{Use: "docs"}
	DocsUpdate.Mount(parent, f)
	parent.SetArgs(args)
	parent.SilenceErrors = true
	parent.SilenceUsage = true
	if stdout != nil {
		stdout.Reset()
	}
	return parent.Execute()
}

// registerMCPStub adds an /mcp stub returning the canned tools/call payload
// once. httpmock matches each stub exactly one time, so a --show-diff run
// (pre-fetch → update → post-fetch) needs three stubs.
func registerMCPStub(reg *httpmock.Registry, payload map[string]interface{}) {
	raw, _ := json.Marshal(payload)
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/mcp",
		Body: map[string]interface{}{
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": string(raw)},
				},
			},
		},
	})
}

// TestDocsUpdateShowDiffEmitsUnifiedDiff wires up three MCP stubs to drive
// the full show-diff path: pre-fetch returns the original markdown, the
// update-doc call reports success, and post-fetch returns the modified
// markdown. The test asserts that the unified diff of the changed region
// lands on stderr while the JSON response stays on stdout.
func TestDocsUpdateShowDiffEmitsUnifiedDiff(t *testing.T) {
	f, stdout, stderr, reg := cmdutil.TestFactory(t, showDiffTestConfig())
	registerMCPStub(reg, map[string]interface{}{"markdown": "line one\noriginal middle\nline three\n"})
	registerMCPStub(reg, map[string]interface{}{"success": true})
	registerMCPStub(reg, map[string]interface{}{"markdown": "line one\nreplacement middle\nline three\n"})

	err := runDocsUpdateShortcut(t, f, stdout, []string{
		"+update",
		"--doc", "DOC123",
		"--mode", "replace_range",
		"--selection-with-ellipsis", "original middle",
		"--markdown", "replacement middle",
		"--show-diff",
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	errOut := stderr.String()
	if !strings.Contains(errOut, "--- before") || !strings.Contains(errOut, "+++ after") {
		t.Errorf("expected unified-diff header on stderr, got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "-original middle") {
		t.Errorf("expected deletion line on stderr, got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "+replacement middle") {
		t.Errorf("expected addition line on stderr, got:\n%s", errOut)
	}
	// The update response JSON must still land on stdout cleanly.
	if !strings.Contains(stdout.String(), "success") {
		t.Errorf("expected update success JSON on stdout, got:\n%s", stdout.String())
	}
}

// TestDocsUpdateShowDiffSkipsForAppendMode ensures append (and overwrite)
// never trigger pre/post fetches — the only /mcp stub registered handles
// the update-doc call itself; any extra fetch would trip 'no stub' in
// httpmock and fail the test.
func TestDocsUpdateShowDiffSkipsForAppendMode(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, showDiffTestConfig())
	registerMCPStub(reg, map[string]interface{}{"success": true})

	err := runDocsUpdateShortcut(t, f, stdout, []string{
		"+update",
		"--doc", "DOC123",
		"--mode", "append",
		"--markdown", "appended paragraph",
		"--show-diff",
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "success") {
		t.Errorf("expected update success on stdout, got:\n%s", stdout.String())
	}
}

// TestDocsUpdateShowDiffPreFetchFailureDegradesGracefully drives the
// pre-fetch into the paginated-snapshot advisory path by registering a
// stub that returns has_more=true (the guard added in Case 9). The update
// itself must still succeed and the warning must land on stderr.
func TestDocsUpdateShowDiffPreFetchFailureDegradesGracefully(t *testing.T) {
	f, stdout, stderr, reg := cmdutil.TestFactory(t, showDiffTestConfig())
	// Two stubs: the pre-fetch returns has_more=true so fetchMarkdownForDiff
	// rejects the partial snapshot, then the real update-doc call runs.
	registerMCPStub(reg, map[string]interface{}{"markdown": "x", "has_more": true})
	registerMCPStub(reg, map[string]interface{}{"success": true})

	err := runDocsUpdateShortcut(t, f, stdout, []string{
		"+update",
		"--doc", "DOC123",
		"--mode", "replace_range",
		"--selection-with-ellipsis", "any",
		"--markdown", "replacement",
		"--show-diff",
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("update should still succeed despite pre-fetch advisory failure, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "--show-diff pre-fetch failed") {
		t.Errorf("expected pre-fetch failure warning on stderr, got:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "success") {
		t.Errorf("expected update success on stdout, got:\n%s", stdout.String())
	}
}

// TestDocsUpdateShowDiffIdenticalSnapshotsNote: pre and post fetch return
// the same markdown, so the diff is empty. The shortcut should emit the
// "no textual change" note instead of a diff block.
func TestDocsUpdateShowDiffIdenticalSnapshotsNote(t *testing.T) {
	f, stdout, stderr, reg := cmdutil.TestFactory(t, showDiffTestConfig())
	registerMCPStub(reg, map[string]interface{}{"markdown": "unchanged\n"})
	registerMCPStub(reg, map[string]interface{}{"success": true})
	registerMCPStub(reg, map[string]interface{}{"markdown": "unchanged\n"})

	err := runDocsUpdateShortcut(t, f, stdout, []string{
		"+update",
		"--doc", "DOC123",
		"--mode", "replace_range",
		"--selection-with-ellipsis", "unchanged",
		"--markdown", "unchanged",
		"--show-diff",
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stderr.String(), "no textual change") {
		t.Errorf("expected no-change note on stderr, got:\n%s", stderr.String())
	}
}
