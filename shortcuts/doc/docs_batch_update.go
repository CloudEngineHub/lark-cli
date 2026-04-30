// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package doc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/larksuite/cli/shortcuts/common"
)

// batchUpdateOp is one entry in the --operations JSON array. The field set
// mirrors the flags of `docs +update` so an operations file is effectively a
// serialized batch of +update invocations against a single document.
//
// Omit-empty is deliberate: a caller may submit an append / overwrite op
// without any selection, and the MCP call itself ignores unset fields.
type batchUpdateOp struct {
	Mode                  string `json:"mode"`
	Markdown              string `json:"markdown,omitempty"`
	SelectionWithEllipsis string `json:"selection_with_ellipsis,omitempty"`
	SelectionByTitle      string `json:"selection_by_title,omitempty"`
	NewTitle              string `json:"new_title,omitempty"`
}

// batchUpdateResult is the per-op entry in the shortcut's JSON response.
// Success is explicit (not derived from the presence of Error) so callers
// can script against a stable schema without having to infer state.
type batchUpdateResult struct {
	Index   int                    `json:"index"`
	Mode    string                 `json:"mode"`
	Success bool                   `json:"success"`
	Error   string                 `json:"error,omitempty"`
	Result  map[string]interface{} `json:"result,omitempty"`
}

var validBatchOnError = map[string]bool{
	"stop":     true,
	"continue": true,
}

// DocsBatchUpdate applies a sequence of update-doc operations to a single
// document. It is an orchestration convenience — there is no server-side
// transaction, so "batch" here means "one CLI call, shared validation,
// unified reporting", not atomic. On a mid-batch failure the document is
// left in a partial-apply state; pair with --on-error=stop + `docs +fetch`
// to recover manually. This tradeoff is explicit in the shortcut's
// description.
//
// Two design notes worth calling out for callers:
//
//  1. Validate runs once up front against the static op list, before any
//     MCP write. It cannot foresee in-batch staleness — for example, if
//     op[0] deletes a section that op[1]'s --selection-by-title resolves
//     into, op[1] passes Validate but fails at execute time. This is by
//     design (sequential semantics; each op sees the doc state produced
//     by the previous op). If you need stricter atomicity, split the
//     batch or fetch the doc between groups of related ops.
//
//  2. On --on-error=stop the shortcut emits TWO failure signals: the
//     stdout JSON envelope (with stopped_early=true and the partial
//     result list), AND a non-zero exit via the returned error. They are
//     complementary: scripts that key on exit code see "the batch did
//     not complete cleanly"; callers parsing JSON see "exactly which
//     ops succeeded and how far we got". Don't treat them as redundant.
var DocsBatchUpdate = common.Shortcut{
	Service:     "docs",
	Command:     "+batch-update",
	Description: "Apply a sequence of update-doc operations to a single document. Sequential execution, not atomic — partial failure leaves the document in a partial-apply state. Pair with --on-error=stop + docs +fetch to recover.",
	Risk:        "write",
	Scopes:      []string{"docx:document:write_only", "docx:document:readonly"},
	AuthTypes:   []string{"user", "bot"},
	Flags: []common.Flag{
		{Name: "doc", Desc: "document URL or token", Required: true},
		{Name: "operations", Desc: "JSON array of operations (each entry: mode, markdown, selection_with_ellipsis, selection_by_title, new_title)", Required: true, Input: []string{common.File, common.Stdin}},
		{Name: "on-error", Default: "stop", Desc: "behavior when a single operation fails: stop | continue"},
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		onError := runtime.Str("on-error")
		if !validBatchOnError[onError] {
			return common.FlagErrorf("invalid --on-error %q, valid: stop | continue", onError)
		}
		ops, err := parseBatchUpdateOps(runtime.Str("operations"))
		if err != nil {
			return err
		}
		for i, op := range ops {
			if err := validateBatchUpdateOp(op); err != nil {
				return common.FlagErrorf("--operations[%d]: %s", i, err.Error())
			}
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		ops, err := parseBatchUpdateOps(runtime.Str("operations"))
		if err != nil {
			return common.NewDryRunAPI().Set("error", err.Error())
		}
		d := common.NewDryRunAPI().
			Desc(fmt.Sprintf("%d-op sequential batch against doc %q; MCP tool: update-doc", len(ops), runtime.Str("doc"))).
			Set("mcp_tool", "update-doc").
			Set("op_count", len(ops))
		mcpEndpoint := common.MCPEndpoint(runtime.Config.Brand)
		for i, op := range ops {
			args := buildBatchUpdateArgs(runtime.Str("doc"), op)
			d.POST(mcpEndpoint).
				Desc(fmt.Sprintf("[%d/%d] %s", i+1, len(ops), op.Mode)).
				Body(map[string]interface{}{
					"method": "tools/call",
					"params": map[string]interface{}{
						"name":      "update-doc",
						"arguments": args,
					},
				})
		}
		return d
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		ops, err := parseBatchUpdateOps(runtime.Str("operations"))
		if err != nil {
			return err
		}
		stopOnError := runtime.Str("on-error") == "stop"
		results := make([]batchUpdateResult, 0, len(ops))
		successCount := 0

		for i, op := range ops {
			// Honor cancellation between ops so a user interrupt or parent
			// timeout doesn't have to wait for the rest of the batch. Emit
			// the partial envelope first so callers see exactly how far we
			// got, mirroring the --on-error=stop shape.
			if err := ctx.Err(); err != nil {
				runtime.Out(map[string]interface{}{
					"doc":           runtime.Str("doc"),
					"total":         len(ops),
					"applied":       successCount,
					"results":       results,
					"stopped_early": true,
				}, nil)
				return err
			}

			// Re-run the same static warnings the single-op shortcut emits so
			// batch users get the same advisory signal per-op.
			for _, w := range docsUpdateWarnings(op.Mode, op.Markdown) {
				fmt.Fprintf(runtime.IO().ErrOut, "warning: --operations[%d]: %s\n", i, w)
			}

			args := buildBatchUpdateArgs(runtime.Str("doc"), op)
			out, callErr := common.CallMCPTool(runtime, "update-doc", args)
			if callErr != nil {
				results = append(results, batchUpdateResult{
					Index: i, Mode: op.Mode, Success: false, Error: callErr.Error(),
				})
				if stopOnError {
					fmt.Fprintf(runtime.IO().ErrOut,
						"error: --operations[%d] failed; stopping (--on-error=stop); %d/%d applied before the failure\n",
						i, successCount, len(ops))
					runtime.Out(map[string]interface{}{
						"doc":           runtime.Str("doc"),
						"total":         len(ops),
						"applied":       successCount,
						"results":       results,
						"stopped_early": true,
					}, nil)
					return callErr
				}
				continue
			}
			normalizeWhiteboardResult(out, op.Markdown)
			results = append(results, batchUpdateResult{
				Index: i, Mode: op.Mode, Success: true, Result: out,
			})
			successCount++
		}

		runtime.Out(map[string]interface{}{
			"doc":           runtime.Str("doc"),
			"total":         len(ops),
			"applied":       successCount,
			"results":       results,
			"stopped_early": false,
		}, nil)
		return nil
	},
}

// buildBatchUpdateArgs constructs the update-doc MCP arguments for one op,
// omitting empty optional fields so the server sees the same shape as a
// single-op `docs +update` call.
func buildBatchUpdateArgs(docID string, op batchUpdateOp) map[string]interface{} {
	args := map[string]interface{}{
		"doc_id": docID,
		"mode":   op.Mode,
	}
	if op.Markdown != "" {
		args["markdown"] = op.Markdown
	}
	if op.SelectionWithEllipsis != "" {
		args["selection_with_ellipsis"] = op.SelectionWithEllipsis
	}
	if op.SelectionByTitle != "" {
		args["selection_by_title"] = op.SelectionByTitle
	}
	if op.NewTitle != "" {
		args["new_title"] = op.NewTitle
	}
	return args
}

// parseBatchUpdateOps accepts a JSON array and returns the typed ops slice
// with a clearer error on the two mistakes users make most often: passing a
// single object instead of an array, or passing an empty array.
func parseBatchUpdateOps(raw string) ([]batchUpdateOp, error) {
	// Strip a leading UTF-8 BOM (U+FEFF) before the prefix check.
	// strings.TrimSpace doesn't treat BOM as whitespace, so payloads written
	// by editors / shells that prepend a BOM (PowerShell redirection, some
	// Windows editors) would otherwise fail the "[" prefix probe with a
	// confusing "must be a JSON array" error.
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\ufeff"))
	if trimmed == "" {
		return nil, common.FlagErrorf("--operations is required")
	}
	if !strings.HasPrefix(trimmed, "[") {
		return nil, common.FlagErrorf("--operations must be a JSON array of operation objects (received object or scalar)")
	}
	var ops []batchUpdateOp
	if err := json.Unmarshal([]byte(trimmed), &ops); err != nil {
		return nil, common.FlagErrorf("--operations is not valid JSON: %s", err.Error())
	}
	if len(ops) == 0 {
		return nil, common.FlagErrorf("--operations must contain at least one operation")
	}
	return ops, nil
}

// validateBatchUpdateOp reuses the same rule set as `docs +update`. Keeping
// it duplicated (rather than factoring the original Validate into a shared
// helper) is a deliberate small trade: the batch shortcut calls this in its
// own Validate phase, before any MCP work, so a single malformed op fails
// the whole invocation up front instead of after N successful ops.
func validateBatchUpdateOp(op batchUpdateOp) error {
	if !validModesV1[op.Mode] {
		return fmt.Errorf("invalid mode %q, valid: append | overwrite | replace_range | replace_all | insert_before | insert_after | delete_range", op.Mode)
	}
	if op.Mode != "delete_range" && op.Markdown == "" {
		return fmt.Errorf("mode=%s requires markdown", op.Mode)
	}
	if op.SelectionWithEllipsis != "" && op.SelectionByTitle != "" {
		return fmt.Errorf("selection_with_ellipsis and selection_by_title are mutually exclusive")
	}
	if needsSelectionV1[op.Mode] && op.SelectionWithEllipsis == "" && op.SelectionByTitle == "" {
		return fmt.Errorf("mode=%s requires selection_with_ellipsis or selection_by_title", op.Mode)
	}
	if op.SelectionByTitle != "" {
		if err := validateSelectionByTitleV1(op.SelectionByTitle); err != nil {
			return err
		}
	}
	return nil
}
