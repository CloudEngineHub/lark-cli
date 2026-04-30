// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package doc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/httpmock"
)

func batchUpdateTestConfig() *core.CliConfig {
	return &core.CliConfig{AppID: "batch-update-test", AppSecret: "test-secret", Brand: core.BrandFeishu}
}

func runBatchUpdateShortcut(t *testing.T, f *cmdutil.Factory, stdout *bytes.Buffer, args []string) error {
	t.Helper()
	parent := &cobra.Command{Use: "docs"}
	DocsBatchUpdate.Mount(parent, f)
	parent.SetArgs(args)
	parent.SilenceErrors = true
	parent.SilenceUsage = true
	if stdout != nil {
		stdout.Reset()
	}
	return parent.Execute()
}

// registerUpdateDocMCPStubs registers one /mcp stub per expected call.
// httpmock matches each stub exactly once, so a batch with N ops needs N
// stubs. Each call gets the same canned payload.
func registerUpdateDocMCPStubs(reg *httpmock.Registry, count int, payload map[string]interface{}) {
	raw, _ := json.Marshal(payload)
	for i := 0; i < count; i++ {
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
}

func TestParseBatchUpdateOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr string
	}{
		{
			name:    "empty input rejected",
			input:   "",
			wantErr: "--operations is required",
		},
		{
			name:    "whitespace-only rejected",
			input:   "   \n\t",
			wantErr: "--operations is required",
		},
		{
			name:    "single object rejected with clear message",
			input:   `{"mode":"append","markdown":"x"}`,
			wantErr: "must be a JSON array",
		},
		{
			name:    "scalar rejected",
			input:   `"append"`,
			wantErr: "must be a JSON array",
		},
		{
			name:    "empty array rejected",
			input:   `[]`,
			wantErr: "at least one operation",
		},
		{
			name:    "malformed JSON surfaced",
			input:   `[{"mode":"append"`,
			wantErr: "not valid JSON",
		},
		{
			name:    "single-op array parses",
			input:   `[{"mode":"append","markdown":"hello"}]`,
			wantLen: 1,
		},
		{
			name: "multi-op array parses",
			input: `[
				{"mode":"replace_range","markdown":"x","selection_with_ellipsis":"a...b"},
				{"mode":"insert_before","markdown":"y","selection_by_title":"## H2"},
				{"mode":"delete_range","selection_with_ellipsis":"z...z"}
			]`,
			wantLen: 3,
		},
		{
			name:    "leading UTF-8 BOM accepted",
			input:   "\ufeff" + `[{"mode":"append","markdown":"hello"}]`,
			wantLen: 1,
		},
		{
			name:    "BOM after surrounding whitespace accepted",
			input:   " \t\ufeff\n" + `[{"mode":"append","markdown":"hello"}]`,
			wantLen: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ops, err := parseBatchUpdateOps(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(ops) != tt.wantLen {
				t.Fatalf("got %d ops, want %d", len(ops), tt.wantLen)
			}
		})
	}
}

func TestValidateBatchUpdateOp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		op      batchUpdateOp
		wantErr string
	}{
		{
			name: "append with markdown is valid",
			op:   batchUpdateOp{Mode: "append", Markdown: "hello"},
		},
		{
			name: "replace_range with selection-with-ellipsis is valid",
			op:   batchUpdateOp{Mode: "replace_range", Markdown: "new", SelectionWithEllipsis: "a...b"},
		},
		{
			name: "replace_range with selection-by-title is valid",
			op:   batchUpdateOp{Mode: "replace_range", Markdown: "new", SelectionByTitle: "## Section"},
		},
		{
			name: "delete_range without markdown is valid",
			op:   batchUpdateOp{Mode: "delete_range", SelectionWithEllipsis: "a...b"},
		},
		{
			name:    "unknown mode rejected",
			op:      batchUpdateOp{Mode: "bogus", Markdown: "x"},
			wantErr: "invalid mode",
		},
		{
			name:    "non-delete mode without markdown rejected",
			op:      batchUpdateOp{Mode: "replace_range", SelectionWithEllipsis: "a...b"},
			wantErr: "requires markdown",
		},
		{
			name:    "both selections rejected",
			op:      batchUpdateOp{Mode: "replace_range", Markdown: "x", SelectionWithEllipsis: "a", SelectionByTitle: "## b"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "selection-requiring mode without any selection rejected",
			op:      batchUpdateOp{Mode: "replace_range", Markdown: "x"},
			wantErr: "requires selection_with_ellipsis or selection_by_title",
		},
		{
			name:    "selection-by-title without leading hash rejected",
			op:      batchUpdateOp{Mode: "replace_range", Markdown: "x", SelectionByTitle: "Section"},
			wantErr: "heading prefix",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateBatchUpdateOp(tt.op)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildBatchUpdateArgs(t *testing.T) {
	t.Parallel()

	t.Run("omits empty optional fields", func(t *testing.T) {
		t.Parallel()
		args := buildBatchUpdateArgs("DOC123", batchUpdateOp{Mode: "append", Markdown: "hello"})
		if _, ok := args["selection_with_ellipsis"]; ok {
			t.Errorf("expected selection_with_ellipsis omitted when empty")
		}
		if _, ok := args["selection_by_title"]; ok {
			t.Errorf("expected selection_by_title omitted when empty")
		}
		if _, ok := args["new_title"]; ok {
			t.Errorf("expected new_title omitted when empty")
		}
		if args["doc_id"] != "DOC123" {
			t.Errorf("expected doc_id DOC123, got %v", args["doc_id"])
		}
		if args["mode"] != "append" {
			t.Errorf("expected mode append, got %v", args["mode"])
		}
	})

	t.Run("carries all set fields", func(t *testing.T) {
		t.Parallel()
		args := buildBatchUpdateArgs("DOC123", batchUpdateOp{
			Mode:                  "replace_range",
			Markdown:              "new",
			SelectionWithEllipsis: "a...b",
			NewTitle:              "Renamed",
		})
		if args["selection_with_ellipsis"] != "a...b" {
			t.Errorf("expected selection_with_ellipsis a...b")
		}
		if args["new_title"] != "Renamed" {
			t.Errorf("expected new_title Renamed")
		}
	})

	t.Run("delete_range without markdown omits the key", func(t *testing.T) {
		t.Parallel()
		args := buildBatchUpdateArgs("DOC123", batchUpdateOp{
			Mode:                  "delete_range",
			SelectionWithEllipsis: "a...b",
		})
		if _, ok := args["markdown"]; ok {
			t.Errorf("expected markdown omitted for delete_range with empty markdown")
		}
	})
}

// TestDocsBatchUpdateDryRun exercises the DryRun branch end-to-end: the
// output must describe the op count and list one POST step per operation,
// covering both the multi-op orchestration and the dry-run argument
// construction path that mirrors Execute.
func TestDocsBatchUpdateDryRun(t *testing.T) {
	t.Parallel()

	f, stdout, _, _ := cmdutil.TestFactory(t, batchUpdateTestConfig())

	ops := `[
		{"mode":"replace_range","markdown":"A","selection_with_ellipsis":"old...A"},
		{"mode":"insert_before","markdown":"B","selection_by_title":"## Intro"},
		{"mode":"delete_range","selection_with_ellipsis":"stale...end"}
	]`
	err := runBatchUpdateShortcut(t, f, stdout, []string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", ops,
		"--dry-run",
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "3-op sequential batch") {
		t.Errorf("dry-run should describe 3-op batch, got: %s", out)
	}
	if !strings.Contains(out, `"op_count": 3`) && !strings.Contains(out, `"op_count":3`) {
		t.Errorf("dry-run should set op_count=3, got: %s", out)
	}
	// Each op index appears as [i/N] inside a step desc.
	for _, prefix := range []string{"[1/3] replace_range", "[2/3] insert_before", "[3/3] delete_range"} {
		if !strings.Contains(out, prefix) {
			t.Errorf("dry-run missing step %q; got: %s", prefix, out)
		}
	}
}

// TestDocsBatchUpdateValidateRejectsMalformedOp ensures a bad op inside
// --operations short-circuits before any MCP call. Covers the per-op
// Validate loop path that the parse/validate unit tests alone don't
// exercise from the CLI entry point.
func TestDocsBatchUpdateValidateRejectsMalformedOp(t *testing.T) {
	t.Parallel()

	f, _, _, _ := cmdutil.TestFactory(t, batchUpdateTestConfig())
	err := runBatchUpdateShortcut(t, f, nil, []string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", `[{"mode":"replace_range","markdown":"x"}]`, // missing selection
		"--dry-run",
		"--as", "bot",
	})
	if err == nil {
		t.Fatalf("expected validation error for missing selection, got nil")
	}
	if !strings.Contains(err.Error(), "requires selection_with_ellipsis or selection_by_title") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestDocsBatchUpdateValidateRejectsInvalidOnError(t *testing.T) {
	t.Parallel()

	f, _, _, _ := cmdutil.TestFactory(t, batchUpdateTestConfig())
	err := runBatchUpdateShortcut(t, f, nil, []string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", `[{"mode":"append","markdown":"x"}]`,
		"--on-error", "panic-on-everything",
		"--dry-run",
		"--as", "bot",
	})
	if err == nil {
		t.Fatalf("expected validation error for bad --on-error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid --on-error") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestDocsBatchUpdateExecuteAllSuccess mocks update-doc to succeed and
// verifies the batch response shape: total, applied, stopped_early=false,
// and per-op success entries.
func TestDocsBatchUpdateExecuteAllSuccess(t *testing.T) {
	t.Parallel()

	f, stdout, _, reg := cmdutil.TestFactory(t, batchUpdateTestConfig())
	registerUpdateDocMCPStubs(reg, 2, map[string]interface{}{
		"success": true,
		"message": "ok",
	})

	ops := `[
		{"mode":"append","markdown":"first"},
		{"mode":"append","markdown":"second"}
	]`
	err := runBatchUpdateShortcut(t, f, stdout, []string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", ops,
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var envelope struct {
		Data struct {
			Total        int  `json:"total"`
			Applied      int  `json:"applied"`
			StoppedEarly bool `json:"stopped_early"`
			Results      []struct {
				Index   int    `json:"index"`
				Mode    string `json:"mode"`
				Success bool   `json:"success"`
				Error   string `json:"error"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse stdout JSON: %v\nstdout:\n%s", err, stdout.String())
	}
	if envelope.Data.Total != 2 || envelope.Data.Applied != 2 {
		t.Fatalf("expected total=2 applied=2, got total=%d applied=%d", envelope.Data.Total, envelope.Data.Applied)
	}
	if envelope.Data.StoppedEarly {
		t.Errorf("expected stopped_early=false for all-success run")
	}
	if len(envelope.Data.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(envelope.Data.Results))
	}
	for i, r := range envelope.Data.Results {
		if r.Index != i || !r.Success || r.Error != "" {
			t.Errorf("result[%d] = %+v; want success=true, error=\"\"", i, r)
		}
	}
}

// TestDocsBatchUpdateStopsOnFirstFailure registers only one successful MCP
// stub for a 2-op batch, so the second call trips "no stub" in httpmock and
// surfaces as an MCP error. With --on-error=stop (the default), the batch
// must halt, report applied=1, and set stopped_early=true.
func TestDocsBatchUpdateStopsOnFirstFailure(t *testing.T) {
	// Explicitly no t.Parallel(): this test ends with an unmatched stub on
	// the failure path, and the parent TestFactory registry.Verify() will
	// flag unused stubs across siblings if the parallel schedule bleeds.
	f, stdout, _, reg := cmdutil.TestFactory(t, batchUpdateTestConfig())
	registerUpdateDocMCPStubs(reg, 1, map[string]interface{}{
		"success": true,
	})

	ops := `[
		{"mode":"append","markdown":"first"},
		{"mode":"append","markdown":"second"}
	]`
	err := runBatchUpdateShortcut(t, f, stdout, []string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", ops,
		"--as", "bot",
	})
	if err == nil {
		t.Fatalf("expected error from second op failing, got nil")
	}

	// Even on error the shortcut prints its partial result envelope first.
	var envelope struct {
		Data struct {
			Total        int  `json:"total"`
			Applied      int  `json:"applied"`
			StoppedEarly bool `json:"stopped_early"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse partial result: %v\nstdout:\n%s", err, stdout.String())
	}
	if envelope.Data.Total != 2 {
		t.Errorf("expected total=2, got %d", envelope.Data.Total)
	}
	if envelope.Data.Applied != 1 {
		t.Errorf("expected applied=1 before stop, got %d", envelope.Data.Applied)
	}
	if !envelope.Data.StoppedEarly {
		t.Errorf("expected stopped_early=true, got false")
	}
}

// TestDocsBatchUpdateContinuesOnError exercises --on-error=continue for a
// 3-op batch where the middle op fails: a later op must still run, the
// envelope's per-op results must mark the failed index with an error
// string, and stopped_early must stay false. Regression for the original
// review note that the suite covered stop-on-error but not the continue
// mode.
func TestDocsBatchUpdateContinuesOnError(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, batchUpdateTestConfig())
	// httpmock matches stubs in registration order, one per call. Register:
	//   stub[0] → op[0] success
	//   stub[1] → op[1] returns HTTP 500 (so CallMCPTool surfaces an error)
	//   stub[2] → op[2] success (proves the loop continued past op[1])
	registerUpdateDocMCPStubs(reg, 1, map[string]interface{}{"success": true, "applied": "first"})
	reg.Register(&httpmock.Stub{
		Method: "POST",
		URL:    "/mcp",
		Status: 500,
		Body:   map[string]interface{}{"error": map[string]interface{}{"message": "synthetic op[1] failure"}},
	})
	registerUpdateDocMCPStubs(reg, 1, map[string]interface{}{"success": true, "applied": "third"})

	ops := `[
		{"mode":"append","markdown":"first"},
		{"mode":"append","markdown":"second"},
		{"mode":"append","markdown":"third"}
	]`
	err := runBatchUpdateShortcut(t, f, stdout, []string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", ops,
		"--on-error", "continue",
		"--as", "bot",
	})
	if err != nil {
		t.Fatalf("--on-error=continue must not surface a top-level error, got: %v", err)
	}

	var envelope struct {
		Data struct {
			Total        int  `json:"total"`
			Applied      int  `json:"applied"`
			StoppedEarly bool `json:"stopped_early"`
			Results      []struct {
				Index   int    `json:"index"`
				Mode    string `json:"mode"`
				Success bool   `json:"success"`
				Error   string `json:"error"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse stdout JSON: %v\nstdout:\n%s", err, stdout.String())
	}
	if envelope.Data.Total != 3 {
		t.Errorf("expected total=3, got %d", envelope.Data.Total)
	}
	if envelope.Data.Applied != 2 {
		t.Errorf("expected applied=2 (op[0] and op[2] succeed, op[1] fails), got %d", envelope.Data.Applied)
	}
	if envelope.Data.StoppedEarly {
		t.Errorf("expected stopped_early=false for continue-on-error, got true")
	}
	if len(envelope.Data.Results) != 3 {
		t.Fatalf("expected 3 per-op results, got %d", len(envelope.Data.Results))
	}
	if !envelope.Data.Results[0].Success || envelope.Data.Results[0].Error != "" {
		t.Errorf("results[0] should be success with no error, got %+v", envelope.Data.Results[0])
	}
	if envelope.Data.Results[1].Success || envelope.Data.Results[1].Error == "" {
		t.Errorf("results[1] should be failure with error, got %+v", envelope.Data.Results[1])
	}
	if !envelope.Data.Results[2].Success || envelope.Data.Results[2].Error != "" {
		t.Errorf("results[2] should be success with no error (continue ran op[2] after op[1] failed), got %+v", envelope.Data.Results[2])
	}
}

// TestDocsBatchUpdateRespectsContextCancellation verifies the Execute loop
// honors a cancelled context: with a pre-cancelled context and zero MCP
// stubs registered, the shortcut must surface context.Canceled and emit a
// partial envelope (applied=0, stopped_early=true) without making any MCP
// calls. Regression for the per-iteration ctx.Err() check.
func TestDocsBatchUpdateRespectsContextCancellation(t *testing.T) {
	f, stdout, _, _ := cmdutil.TestFactory(t, batchUpdateTestConfig())

	parent := &cobra.Command{Use: "docs"}
	DocsBatchUpdate.Mount(parent, f)
	parent.SetArgs([]string{
		"+batch-update",
		"--doc", "DOC123",
		"--operations", `[{"mode":"append","markdown":"a"},{"mode":"append","markdown":"b"}]`,
		"--as", "bot",
	})
	parent.SilenceErrors = true
	parent.SilenceUsage = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := parent.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	var envelope struct {
		Data struct {
			Total        int  `json:"total"`
			Applied      int  `json:"applied"`
			StoppedEarly bool `json:"stopped_early"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse partial envelope: %v\nstdout:\n%s", err, stdout.String())
	}
	if envelope.Data.Total != 2 {
		t.Errorf("expected total=2, got %d", envelope.Data.Total)
	}
	if envelope.Data.Applied != 0 {
		t.Errorf("expected applied=0 (cancelled before any MCP call), got %d", envelope.Data.Applied)
	}
	if !envelope.Data.StoppedEarly {
		t.Errorf("expected stopped_early=true on cancellation, got false")
	}
}
