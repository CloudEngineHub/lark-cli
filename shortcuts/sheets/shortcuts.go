// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import "github.com/larksuite/cli/shortcuts/common"

// Shortcuts returns all lark-sheets shortcuts. The list is grouped by
// canonical skill to mirror the sheet-skill-spec layout
// (lark_sheet_workbook → lark_sheet_float_image).
func Shortcuts() []common.Shortcut {
	return []common.Shortcut{
		// lark_sheet_workbook
		WorkbookInfo,
		SheetCreate,
		SheetDelete,
		SheetRename,
		SheetMove,
		SheetCopy,
		SheetHide,
		SheetUnhide,
		SheetSetTabColor,
	}
}
