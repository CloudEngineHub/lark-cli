// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"context"

	"github.com/larksuite/cli/shortcuts/common"
)

// WorkbookInfo wraps the get_workbook_structure tool: list a workbook's
// sub-sheets with their metadata (sheet_id, title, dimensions, freeze rows
// and cols, index, hidden). This is the first step for every sheets task —
// downstream sheet-level operations all depend on the sheet_id returned here.
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
