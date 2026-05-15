// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"context"
	"fmt"
	"strings"

	"github.com/larksuite/cli/internal/output"
	"github.com/larksuite/cli/internal/util"
	"github.com/larksuite/cli/shortcuts/common"
)

// ─── lark_sheet_workbook ──────────────────────────────────────────────
//
// Wraps two tools behind the One-OpenAPI: get_workbook_structure (read) and
// modify_workbook_structure (write, dispatched by `operation` enum).
//
// CLI Risk tiers diverge intentionally from the tool's single endpoint:
//   - +sheet-delete  is high-risk-write (irreversible)
//   - everything else is plain write
//
// +sheet-create only carries --url / --spreadsheet-token (no sheet selector):
// the create tool path needs no existing-sheet anchor, so the public sheet
// selector pair is dropped here to avoid a misleading XOR requirement.

// WorkbookInfo wraps get_workbook_structure: list a workbook's sub-sheets
// with their metadata (sheet_id, title, dimensions, freeze rows and cols,
// index, hidden). First step for every sheets task — downstream sheet-level
// operations all depend on the sheet_id returned here.
var WorkbookInfo = common.Shortcut{
	Service:     "sheets",
	Command:     "+workbook-info",
	Description: "List sub-sheets of a spreadsheet with metadata (sheet_id, title, dimensions, freeze, hidden).",
	Risk:        "read",
	Scopes:      []string{"sheets:spreadsheet:read"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags:       publicTokenFlags(),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := resolveSpreadsheetToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		return invokeToolDryRun(token, ToolKindRead, "get_workbook_structure", map[string]interface{}{
			"excel_id": token,
		})
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		out, err := callTool(ctx, runtime, token, ToolKindRead, "get_workbook_structure", map[string]interface{}{
			"excel_id": token,
		})
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
	Tips: []string{
		"First step for every sheets task — capture sheet_id from the result before doing any sheet-level operation.",
	},
}

// SheetCreate creates a new sub-sheet. --title is the new sheet's name;
// --index inserts at a specific position (omitted → appended). Default
// dimensions match the canonical schema (rows=100, cols=26 when omitted —
// tool's defaults differ but CLI surface stays predictable).
var SheetCreate = common.Shortcut{
	Service:     "sheets",
	Command:     "+sheet-create",
	Description: "Create a new sub-sheet with an optional position and initial dimensions.",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicTokenFlags(),
		common.Flag{Name: "title", Desc: "new sheet title", Required: true},
		common.Flag{Name: "index", Type: "int", Default: "-1", Desc: "insertion position (0-based); omit to append"},
		common.Flag{Name: "row-count", Type: "int", Default: "0", Desc: "initial row count; omit for tool default (200)"},
		common.Flag{Name: "col-count", Type: "int", Default: "0", Desc: "initial column count; omit for tool default (20)"},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		if strings.TrimSpace(runtime.Str("title")) == "" {
			return common.FlagErrorf("--title is required")
		}
		if n := runtime.Int("row-count"); n < 0 || n > 50000 {
			return common.FlagErrorf("--row-count must be between 0 and 50000")
		}
		if n := runtime.Int("col-count"); n < 0 || n > 200 {
			return common.FlagErrorf("--col-count must be between 0 and 200")
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", sheetCreateInput(runtime, token))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", sheetCreateInput(runtime, token))
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

func sheetCreateInput(runtime *common.RuntimeContext, token string) map[string]interface{} {
	input := map[string]interface{}{
		"excel_id":   token,
		"operation":  "create",
		"sheet_name": strings.TrimSpace(runtime.Str("title")),
	}
	if runtime.Changed("index") {
		input["target_index"] = runtime.Int("index")
	}
	if n := runtime.Int("row-count"); n > 0 {
		input["rows"] = n
	}
	if n := runtime.Int("col-count"); n > 0 {
		input["columns"] = n
	}
	return input
}

// SheetDelete deletes a sub-sheet. high-risk-write — framework rejects
// without --yes. Always preview with --dry-run first to confirm the target.
var SheetDelete = common.Shortcut{
	Service:     "sheets",
	Command:     "+sheet-delete",
	Description: "Delete a sub-sheet (irreversible).",
	Risk:        "high-risk-write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags:       publicSheetFlags(),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		_, _, err := resolveSheetSelector(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		input := map[string]interface{}{"excel_id": token, "operation": "delete"}
		sheetSelectorForToolInput(input, sheetID, sheetName)
		return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", input)
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		sheetID, sheetName, err := resolveSheetSelector(runtime)
		if err != nil {
			return err
		}
		input := map[string]interface{}{"excel_id": token, "operation": "delete"}
		sheetSelectorForToolInput(input, sheetID, sheetName)
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
	Tips: []string{
		"Sheet deletion is irreversible. Always run with --dry-run first to verify the target sheet_id/sheet_name.",
	},
}

// SheetRename renames a sub-sheet via --title (mapped to tool's new_name).
var SheetRename = common.Shortcut{
	Service:     "sheets",
	Command:     "+sheet-rename",
	Description: "Rename a sub-sheet.",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicSheetFlags(),
		common.Flag{Name: "title", Desc: "new sheet title", Required: true},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		if _, _, err := resolveSheetSelector(runtime); err != nil {
			return err
		}
		if strings.TrimSpace(runtime.Str("title")) == "" {
			return common.FlagErrorf("--title is required")
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		input := map[string]interface{}{
			"excel_id":  token,
			"operation": "rename",
			"new_name":  strings.TrimSpace(runtime.Str("title")),
		}
		sheetSelectorForToolInput(input, sheetID, sheetName)
		return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", input)
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		sheetID, sheetName, err := resolveSheetSelector(runtime)
		if err != nil {
			return err
		}
		input := map[string]interface{}{
			"excel_id":  token,
			"operation": "rename",
			"new_name":  strings.TrimSpace(runtime.Str("title")),
		}
		sheetSelectorForToolInput(input, sheetID, sheetName)
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

// SheetMove moves a sub-sheet to a new index. The tool requires sheet_id
// and source_index in addition to target_index. The CLI accepts:
//   - --sheet-id / --sheet-name to identify the sheet
//   - --source-index (optional) for explicit source position
//
// When --source-index is omitted, or when --sheet-name is used instead of
// --sheet-id, Execute issues a single get_workbook_structure read to derive
// the missing pieces. DryRun stays network-free: it uses <resolve> placeholders
// for any field that would need that read.
var SheetMove = common.Shortcut{
	Service:     "sheets",
	Command:     "+sheet-move",
	Description: "Move a sub-sheet to a new position.",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:read", "sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicSheetFlags(),
		common.Flag{Name: "index", Type: "int", Required: true, Desc: "target position (0-based)"},
		common.Flag{Name: "source-index", Type: "int", Default: "-1", Desc: "source position (0-based); omitted → auto-derived from --sheet-id/--sheet-name's current workbook position"},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		if _, _, err := resolveSheetSelector(runtime); err != nil {
			return err
		}
		if !runtime.Changed("index") {
			return common.FlagErrorf("--index is required")
		}
		if runtime.Int("index") < 0 {
			return common.FlagErrorf("--index must be >= 0")
		}
		if runtime.Changed("source-index") && runtime.Int("source-index") < 0 {
			return common.FlagErrorf("--source-index must be >= 0")
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		input := map[string]interface{}{
			"excel_id":     token,
			"operation":    "move",
			"sheet_id":     sheetSelectorPlaceholder(sheetID, sheetName),
			"target_index": runtime.Int("index"),
			"source_index": sourceIndexOrPlaceholder(runtime),
		}
		return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", input)
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		sheetID, sheetName, err := resolveSheetSelector(runtime)
		if err != nil {
			return err
		}

		resolvedID := sheetID
		var sourceIndex int
		needIDLookup := sheetID == ""
		needIndexLookup := !runtime.Changed("source-index")
		if needIDLookup || needIndexLookup {
			lookedID, lookedIdx, err := lookupSheetIndex(ctx, runtime, token, sheetID, sheetName)
			if err != nil {
				return err
			}
			resolvedID = lookedID
			sourceIndex = lookedIdx
		}
		if runtime.Changed("source-index") {
			sourceIndex = runtime.Int("source-index")
		}

		input := map[string]interface{}{
			"excel_id":     token,
			"operation":    "move",
			"sheet_id":     resolvedID,
			"source_index": sourceIndex,
			"target_index": runtime.Int("index"),
		}
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
	Tips: []string{
		"Pass --source-index when you already know it to avoid the extra read; otherwise CLI derives it from --sheet-id/--sheet-name.",
	},
}

// sourceIndexOrPlaceholder returns the user-supplied source-index, or the
// string "<resolve>" when DryRun should signal that Execute will derive it.
func sourceIndexOrPlaceholder(runtime *common.RuntimeContext) interface{} {
	if runtime.Changed("source-index") {
		return runtime.Int("source-index")
	}
	return "<resolve>"
}

// SheetCopy duplicates a sub-sheet. --title (optional) names the copy;
// --index (optional) places it.
var SheetCopy = common.Shortcut{
	Service:     "sheets",
	Command:     "+sheet-copy",
	Description: "Duplicate a sub-sheet, optionally renaming and repositioning the copy.",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicSheetFlags(),
		common.Flag{Name: "title", Desc: "title for the duplicated sheet (server-generated when omitted)"},
		common.Flag{Name: "index", Type: "int", Default: "-1", Desc: "insertion position for the copy (0-based); omit to append"},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		_, _, err := resolveSheetSelector(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", sheetCopyInput(runtime, token, sheetID, sheetName))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		sheetID, sheetName, err := resolveSheetSelector(runtime)
		if err != nil {
			return err
		}
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", sheetCopyInput(runtime, token, sheetID, sheetName))
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

func sheetCopyInput(runtime *common.RuntimeContext, token, sheetID, sheetName string) map[string]interface{} {
	input := map[string]interface{}{"excel_id": token, "operation": "duplicate"}
	sheetSelectorForToolInput(input, sheetID, sheetName)
	if t := strings.TrimSpace(runtime.Str("title")); t != "" {
		input["new_name"] = t
	}
	if runtime.Changed("index") {
		input["target_index"] = runtime.Int("index")
	}
	return input
}

// SheetHide / SheetUnhide toggle visibility. Visible bool semantics live in
// the operation enum so callers don't need a --visible flag.
var SheetHide = newSheetVisibilityShortcut(
	"+sheet-hide", "Hide a sub-sheet from the tabs bar.", "hide",
)

var SheetUnhide = newSheetVisibilityShortcut(
	"+sheet-unhide", "Restore a hidden sub-sheet.", "unhide",
)

func newSheetVisibilityShortcut(command, desc, op string) common.Shortcut {
	return common.Shortcut{
		Service:     "sheets",
		Command:     command,
		Description: desc,
		Risk:        "write",
		Scopes:      []string{"sheets:spreadsheet:write_only"},
		AuthTypes:   []string{"user", "bot"},
		HasFormat:   true,
		Flags:       publicSheetFlags(),
		Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
			if _, err := resolveSpreadsheetToken(runtime); err != nil {
				return err
			}
			_, _, err := resolveSheetSelector(runtime)
			return err
		},
		DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
			token, _ := resolveSpreadsheetToken(runtime)
			sheetID, sheetName, _ := resolveSheetSelector(runtime)
			input := map[string]interface{}{"excel_id": token, "operation": op}
			sheetSelectorForToolInput(input, sheetID, sheetName)
			return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", input)
		},
		Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
			token, err := resolveSpreadsheetToken(runtime)
			if err != nil {
				return err
			}
			sheetID, sheetName, err := resolveSheetSelector(runtime)
			if err != nil {
				return err
			}
			input := map[string]interface{}{"excel_id": token, "operation": op}
			sheetSelectorForToolInput(input, sheetID, sheetName)
			out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", input)
			if err != nil {
				return err
			}
			runtime.Out(out, nil)
			return nil
		},
	}
}

// SheetSetTabColor sets the tab color of a sub-sheet. --color "" clears.
var SheetSetTabColor = common.Shortcut{
	Service:     "sheets",
	Command:     "+sheet-set-tab-color",
	Description: "Set or clear the tab color of a sub-sheet.",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicSheetFlags(),
		common.Flag{Name: "color", Desc: "hex color like #FF0000; pass empty string to clear"},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		if _, _, err := resolveSheetSelector(runtime); err != nil {
			return err
		}
		if !runtime.Changed("color") {
			return common.FlagErrorf("--color is required (empty string clears)")
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		input := map[string]interface{}{
			"excel_id":  token,
			"operation": "set_tab_color",
			"tab_color": runtime.Str("color"),
		}
		sheetSelectorForToolInput(input, sheetID, sheetName)
		return invokeToolDryRun(token, ToolKindWrite, "modify_workbook_structure", input)
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetToken(runtime)
		if err != nil {
			return err
		}
		sheetID, sheetName, err := resolveSheetSelector(runtime)
		if err != nil {
			return err
		}
		input := map[string]interface{}{
			"excel_id":  token,
			"operation": "set_tab_color",
			"tab_color": runtime.Str("color"),
		}
		sheetSelectorForToolInput(input, sheetID, sheetName)
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "modify_workbook_structure", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

// lookupSheetIndex finds a sub-sheet by id or name and returns its canonical
// id + current 0-based index. Caller is responsible for ensuring at least one
// of sheetID/sheetName is non-empty.
func lookupSheetIndex(ctx context.Context, runtime *common.RuntimeContext, token, sheetID, sheetName string) (resolvedID string, index int, err error) {
	out, err := callTool(ctx, runtime, token, ToolKindRead, "get_workbook_structure", map[string]interface{}{
		"excel_id": token,
	})
	if err != nil {
		return "", 0, err
	}
	m, ok := out.(map[string]interface{})
	if !ok {
		return "", 0, output.Errorf(output.ExitAPI, "tool_output", "get_workbook_structure returned non-object output")
	}
	sheets, _ := m["sheets"].([]interface{})
	for _, raw := range sheets {
		sm, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := sm["sheet_id"].(string)
		name, _ := sm["sheet_name"].(string)
		if (sheetID != "" && id == sheetID) || (sheetName != "" && name == sheetName) {
			idx, ok := util.ToFloat64(sm["index"])
			if !ok {
				return "", 0, output.Errorf(output.ExitAPI, "tool_output", "sheet entry missing index field")
			}
			return id, int(idx), nil
		}
	}
	target := sheetID
	if target == "" {
		target = sheetName
	}
	return "", 0, output.Errorf(output.ExitAPI, "not_found", fmt.Sprintf("sheet %q not found in workbook", target))
}
