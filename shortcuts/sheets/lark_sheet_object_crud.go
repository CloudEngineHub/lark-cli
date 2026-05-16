// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"context"
	"strings"

	"github.com/larksuite/cli/shortcuts/common"
)

// ─── object CRUD shortcuts ────────────────────────────────────────────
//
// Six object skills (chart / pivot table / conditional format / filter /
// filter view / sparkline / float image) each expose a uniform create /
// update / delete trio backed by their manage_<obj>_object tool.
//
// Shared shape:
//   excel_id + sheet_id|sheet_name + operation + [<obj>_id] + [properties]
//
// CLI `--data` is passed through as the tool's `properties` payload as-is —
// callers shape it per the spec doc for each object (which is what makes
// the surface narrow even though everything funnels through one tool).
//
// Five of the seven objects share the factory below (newObjectCRUDShortcuts).
// pivot adds optional --target-sheet-id / --target-position on create,
// declared with extraCreateFlags. filter is special-cased further down
// (no separate id flag — filter_id is implicit per sheet — and --range is
// a first-class create flag, not buried in --data).

// objectCRUDSpec describes a 3-shortcut create/update/delete cluster.
// idFlag / idField empty → no per-object id flag (only filter uses that
// today, and it has its own bespoke shortcuts further down).
type objectCRUDSpec struct {
	commandPrefix    string // e.g. "+chart" → +chart-create / -update / -delete
	toolName         string // e.g. "manage_chart_object"
	idFlag           string // e.g. "chart-id"
	idField          string // e.g. "chart_id"
	createDataDesc   string // help text for --data on create
	updateDataDesc   string // help text for --data on update
	createExtraFlags []common.Flag
	// createExtraInput, when set, mutates the tool input after the standard
	// fields are written. Used by pivot to inject --target-sheet-id /
	// --target-position alongside properties.
	createExtraInput func(rt *common.RuntimeContext, input map[string]interface{})
}

func newObjectCreateShortcut(spec objectCRUDSpec) common.Shortcut {
	flags := append(publicSheetFlags(),
		common.Flag{Name: "data", Input: []string{common.File, common.Stdin}, Required: true, Desc: spec.createDataDesc},
	)
	flags = append(flags, spec.createExtraFlags...)
	return common.Shortcut{
		Service:     "sheets",
		Command:     spec.commandPrefix + "-create",
		Description: "Create a " + strings.TrimPrefix(spec.commandPrefix, "+") + " object via the manage_*_object tool.",
		Risk:        "write",
		Scopes:      []string{"sheets:spreadsheet:write_only"},
		AuthTypes:   []string{"user", "bot"},
		HasFormat:   true,
		Flags:       flags,
		Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
			if _, err := resolveSpreadsheetToken(runtime); err != nil {
				return err
			}
			if _, _, err := resolveSheetSelector(runtime); err != nil {
				return err
			}
			_, err := requireJSONObject(runtime, "data")
			return err
		},
		DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
			token, _ := resolveSpreadsheetToken(runtime)
			sheetID, sheetName, _ := resolveSheetSelector(runtime)
			input, _ := objectCreateInput(runtime, token, sheetID, sheetName, spec)
			return invokeToolDryRun(token, ToolKindWrite, spec.toolName, input)
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
			input, err := objectCreateInput(runtime, token, sheetID, sheetName, spec)
			if err != nil {
				return err
			}
			out, err := callTool(ctx, runtime, token, ToolKindWrite, spec.toolName, input)
			if err != nil {
				return err
			}
			runtime.Out(out, nil)
			return nil
		},
	}
}

func objectCreateInput(runtime *common.RuntimeContext, token, sheetID, sheetName string, spec objectCRUDSpec) (map[string]interface{}, error) {
	props, err := requireJSONObject(runtime, "data")
	if err != nil {
		return nil, err
	}
	input := map[string]interface{}{
		"excel_id":   token,
		"operation":  "create",
		"properties": props,
	}
	sheetSelectorForToolInput(input, sheetID, sheetName)
	if spec.createExtraInput != nil {
		spec.createExtraInput(runtime, input)
	}
	return input, nil
}

func newObjectUpdateShortcut(spec objectCRUDSpec) common.Shortcut {
	flags := publicSheetFlags()
	if spec.idFlag != "" {
		flags = append(flags, common.Flag{
			Name: spec.idFlag, Required: true,
			Desc: "target object reference_id (maps to " + spec.idField + " on the wire)",
		})
	}
	flags = append(flags, common.Flag{
		Name: "data", Input: []string{common.File, common.Stdin}, Required: true,
		Desc: spec.updateDataDesc,
	})
	return common.Shortcut{
		Service:     "sheets",
		Command:     spec.commandPrefix + "-update",
		Description: "Update an existing " + strings.TrimPrefix(spec.commandPrefix, "+") + " object (read-modify-write; consult --list first).",
		Risk:        "write",
		Scopes:      []string{"sheets:spreadsheet:write_only"},
		AuthTypes:   []string{"user", "bot"},
		HasFormat:   true,
		Flags:       flags,
		Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
			if _, err := resolveSpreadsheetToken(runtime); err != nil {
				return err
			}
			if _, _, err := resolveSheetSelector(runtime); err != nil {
				return err
			}
			if spec.idFlag != "" && strings.TrimSpace(runtime.Str(spec.idFlag)) == "" {
				return common.FlagErrorf("--%s is required", spec.idFlag)
			}
			_, err := requireJSONObject(runtime, "data")
			return err
		},
		DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
			token, _ := resolveSpreadsheetToken(runtime)
			sheetID, sheetName, _ := resolveSheetSelector(runtime)
			input, _ := objectUpdateInput(runtime, token, sheetID, sheetName, spec)
			return invokeToolDryRun(token, ToolKindWrite, spec.toolName, input)
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
			input, err := objectUpdateInput(runtime, token, sheetID, sheetName, spec)
			if err != nil {
				return err
			}
			out, err := callTool(ctx, runtime, token, ToolKindWrite, spec.toolName, input)
			if err != nil {
				return err
			}
			runtime.Out(out, nil)
			return nil
		},
	}
}

func objectUpdateInput(runtime *common.RuntimeContext, token, sheetID, sheetName string, spec objectCRUDSpec) (map[string]interface{}, error) {
	props, err := requireJSONObject(runtime, "data")
	if err != nil {
		return nil, err
	}
	input := map[string]interface{}{
		"excel_id":   token,
		"operation":  "update",
		"properties": props,
	}
	sheetSelectorForToolInput(input, sheetID, sheetName)
	if spec.idFlag != "" {
		input[spec.idField] = strings.TrimSpace(runtime.Str(spec.idFlag))
	}
	return input, nil
}

func newObjectDeleteShortcut(spec objectCRUDSpec) common.Shortcut {
	flags := publicSheetFlags()
	if spec.idFlag != "" {
		flags = append(flags, common.Flag{
			Name: spec.idFlag, Required: true,
			Desc: "target object reference_id (maps to " + spec.idField + " on the wire)",
		})
	}
	return common.Shortcut{
		Service:     "sheets",
		Command:     spec.commandPrefix + "-delete",
		Description: "Delete a " + strings.TrimPrefix(spec.commandPrefix, "+") + " object (irreversible).",
		Risk:        "high-risk-write",
		Scopes:      []string{"sheets:spreadsheet:write_only"},
		AuthTypes:   []string{"user", "bot"},
		HasFormat:   true,
		Flags:       flags,
		Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
			if _, err := resolveSpreadsheetToken(runtime); err != nil {
				return err
			}
			if _, _, err := resolveSheetSelector(runtime); err != nil {
				return err
			}
			if spec.idFlag != "" && strings.TrimSpace(runtime.Str(spec.idFlag)) == "" {
				return common.FlagErrorf("--%s is required", spec.idFlag)
			}
			return nil
		},
		DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
			token, _ := resolveSpreadsheetToken(runtime)
			sheetID, sheetName, _ := resolveSheetSelector(runtime)
			return invokeToolDryRun(token, ToolKindWrite, spec.toolName, objectDeleteInput(runtime, token, sheetID, sheetName, spec))
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
			out, err := callTool(ctx, runtime, token, ToolKindWrite, spec.toolName, objectDeleteInput(runtime, token, sheetID, sheetName, spec))
			if err != nil {
				return err
			}
			runtime.Out(out, nil)
			return nil
		},
	}
}

func objectDeleteInput(runtime *common.RuntimeContext, token, sheetID, sheetName string, spec objectCRUDSpec) map[string]interface{} {
	input := map[string]interface{}{
		"excel_id":  token,
		"operation": "delete",
	}
	sheetSelectorForToolInput(input, sheetID, sheetName)
	if spec.idFlag != "" {
		input[spec.idField] = strings.TrimSpace(runtime.Str(spec.idFlag))
	}
	return input
}

// ─── per-object instantiations ────────────────────────────────────────

// chart
var chartSpec = objectCRUDSpec{
	commandPrefix:  "+chart",
	toolName:       "manage_chart_object",
	idFlag:         "chart-id",
	idField:        "chart_id",
	createDataDesc: "chart properties JSON (position / data / properties etc.); see lark-sheets-chart.md for the shape",
	updateDataDesc: "full or partial chart properties JSON (`+chart-list --chart-id <id>` first, then patch)",
}
var ChartCreate = newObjectCreateShortcut(chartSpec)
var ChartUpdate = newObjectUpdateShortcut(chartSpec)
var ChartDelete = newObjectDeleteShortcut(chartSpec)

// pivot — adds --target-sheet-id / --target-position on create
var pivotSpec = objectCRUDSpec{
	commandPrefix:  "+pivot",
	toolName:       "manage_pivot_table_object",
	idFlag:         "pivot-table-id",
	idField:        "pivot_table_id",
	createDataDesc: "pivot table properties JSON: { data_range, rows, columns, values, filters, show_row_grand_total, show_col_grand_total }",
	updateDataDesc: "full or partial pivot properties JSON (`+pivot-list --pivot-table-id <id>` first, then patch)",
	createExtraFlags: []common.Flag{
		{Name: "target-sheet-id", Desc: "destination sheet id for the pivot table; omit to auto-create a fresh sheet (recommended)"},
		{Name: "target-position", Default: "A1", Desc: "destination start cell, default A1"},
	},
	createExtraInput: func(rt *common.RuntimeContext, input map[string]interface{}) {
		if v := strings.TrimSpace(rt.Str("target-sheet-id")); v != "" {
			input["target_sheet_id"] = v
		}
		if v := strings.TrimSpace(rt.Str("target-position")); v != "" && v != "A1" {
			input["target_position"] = v
		}
	},
}
var PivotCreate = newObjectCreateShortcut(pivotSpec)
var PivotUpdate = newObjectUpdateShortcut(pivotSpec)
var PivotDelete = newObjectDeleteShortcut(pivotSpec)

// conditional format — CLI surface uses --rule-id (short), wired to the
// tool's conditional_format_id on the wire.
var condFormatSpec = objectCRUDSpec{
	commandPrefix:  "+cond-format",
	toolName:       "manage_conditional_format_object",
	idFlag:         "rule-id",
	idField:        "conditional_format_id",
	createDataDesc: "rule JSON: { range, rule: { type: cell_value|duplicate|data_bar|color_scale|rank|formula, ... } }",
	updateDataDesc: "full or partial rule JSON (`+cond-format-list --rule-id <id>` first, then patch)",
}
var CondFormatCreate = newObjectCreateShortcut(condFormatSpec)
var CondFormatUpdate = newObjectUpdateShortcut(condFormatSpec)
var CondFormatDelete = newObjectDeleteShortcut(condFormatSpec)

// sparkline — CLI uses --group-id (higher level) as the object selector.
var sparklineSpec = objectCRUDSpec{
	commandPrefix:  "+sparkline",
	toolName:       "manage_sparkline_object",
	idFlag:         "group-id",
	idField:        "group_id",
	createDataDesc: "sparkline group JSON: { type: line|column|win_loss, source_range, target_range, ... }",
	updateDataDesc: "full or partial sparkline group JSON (`+sparkline-list --group-id <id>` first, then patch)",
}
var SparklineCreate = newObjectCreateShortcut(sparklineSpec)
var SparklineUpdate = newObjectUpdateShortcut(sparklineSpec)
var SparklineDelete = newObjectDeleteShortcut(sparklineSpec)

// float image
var floatImageSpec = objectCRUDSpec{
	commandPrefix:  "+float-image",
	toolName:       "manage_float_image_object",
	idFlag:         "float-image-id",
	idField:        "float_image_id",
	createDataDesc: "float image JSON: { image_uri, image_name, position:{row,col}, size:{width,height}, offset:{x,y} } — image_uri must be pre-uploaded",
	updateDataDesc: "full or partial float image JSON (`+float-image-list --float-image-id <id>` first, then patch)",
}
var FloatImageCreate = newObjectCreateShortcut(floatImageSpec)
var FloatImageUpdate = newObjectUpdateShortcut(floatImageSpec)
var FloatImageDelete = newObjectDeleteShortcut(floatImageSpec)

// filter view — cli_status: cli-only but the tool is in mcp-tools.json so
// it dispatches via the same One-OpenAPI endpoint as every other shortcut.
var filterViewSpec = objectCRUDSpec{
	commandPrefix:  "+filter-view",
	toolName:       "manage_filter_view_object",
	idFlag:         "view-id",
	idField:        "view_id",
	createDataDesc: "filter view JSON: { view_name, range (required, covers header), rules: [...] }",
	updateDataDesc: "partial update JSON: any of { view_name, range, rules }; `+filter-view-list --view-id <id>` first",
}
var FilterViewCreate = newObjectCreateShortcut(filterViewSpec)
var FilterViewUpdate = newObjectUpdateShortcut(filterViewSpec)
var FilterViewDelete = newObjectDeleteShortcut(filterViewSpec)

// ─── filter (sheet-scoped, no separate filter_id) ─────────────────────
//
// At most one filter per sheet, so filter_id is implicit (the tool treats
// filter_id and sheet_id as the same value). create requires --range
// (covering the header) and an optional --data with conditions; update
// patches conditions / range; delete drops the entire filter.

// FilterCreate creates a sheet-level filter. --range covers the data
// (header inclusive). --data is optional — empty filter is valid.
var FilterCreate = common.Shortcut{
	Service:     "sheets",
	Command:     "+filter-create",
	Description: "Create a sheet-level filter (one per sheet).",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicSheetFlags(),
		common.Flag{Name: "range", Required: true, Desc: "filter range including the header row (e.g. A1:F1000)"},
		common.Flag{Name: "data", Input: []string{common.File, common.Stdin},
			Desc: "optional conditions JSON: { conditions: [{col, filter_type, expected, ...}] }; empty filter when omitted"},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		if _, _, err := resolveSheetSelector(runtime); err != nil {
			return err
		}
		if strings.TrimSpace(runtime.Str("range")) == "" {
			return common.FlagErrorf("--range is required")
		}
		if runtime.Str("data") != "" {
			if _, err := requireJSONObject(runtime, "data"); err != nil {
				return err
			}
		}
		return nil
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		input, _ := filterCreateInput(runtime, token, sheetID, sheetName)
		return invokeToolDryRun(token, ToolKindWrite, "manage_filter_object", input)
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
		input, err := filterCreateInput(runtime, token, sheetID, sheetName)
		if err != nil {
			return err
		}
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "manage_filter_object", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

func filterCreateInput(runtime *common.RuntimeContext, token, sheetID, sheetName string) (map[string]interface{}, error) {
	props := map[string]interface{}{
		"range": strings.TrimSpace(runtime.Str("range")),
	}
	if runtime.Str("data") != "" {
		extra, err := requireJSONObject(runtime, "data")
		if err != nil {
			return nil, err
		}
		for k, v := range extra {
			if k == "range" {
				continue // --range wins
			}
			props[k] = v
		}
	}
	input := map[string]interface{}{
		"excel_id":   token,
		"operation":  "create",
		"properties": props,
	}
	sheetSelectorForToolInput(input, sheetID, sheetName)
	return input, nil
}

// FilterUpdate patches the sheet-level filter — change range or
// add/replace conditions. filter_id is implicit (sheet-scoped).
var FilterUpdate = common.Shortcut{
	Service:     "sheets",
	Command:     "+filter-update",
	Description: "Update the sheet-level filter (patch range or conditions).",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: append(publicSheetFlags(),
		common.Flag{Name: "data", Input: []string{common.File, common.Stdin}, Required: true,
			Desc: "patch JSON: { range?, conditions?: [...] } — read with +filter-list first"},
	),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		if _, err := resolveSpreadsheetToken(runtime); err != nil {
			return err
		}
		if _, _, err := resolveSheetSelector(runtime); err != nil {
			return err
		}
		_, err := requireJSONObject(runtime, "data")
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		sheetID, sheetName, _ := resolveSheetSelector(runtime)
		input, _ := filterUpdateInput(runtime, token, sheetID, sheetName)
		return invokeToolDryRun(token, ToolKindWrite, "manage_filter_object", input)
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
		input, err := filterUpdateInput(runtime, token, sheetID, sheetName)
		if err != nil {
			return err
		}
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "manage_filter_object", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

func filterUpdateInput(runtime *common.RuntimeContext, token, sheetID, sheetName string) (map[string]interface{}, error) {
	props, err := requireJSONObject(runtime, "data")
	if err != nil {
		return nil, err
	}
	input := map[string]interface{}{
		"excel_id":   token,
		"operation":  "update",
		"properties": props,
	}
	sheetSelectorForToolInput(input, sheetID, sheetName)
	return input, nil
}

// FilterDelete drops the sheet-level filter entirely. high-risk-write.
var FilterDelete = common.Shortcut{
	Service:     "sheets",
	Command:     "+filter-delete",
	Description: "Remove the sheet-level filter (irreversible).",
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
		return invokeToolDryRun(token, ToolKindWrite, "manage_filter_object", input)
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
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "manage_filter_object", input)
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}
