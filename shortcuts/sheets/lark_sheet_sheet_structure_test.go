// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"strings"
	"testing"

	"github.com/larksuite/cli/shortcuts/common"
)

// TestSheetStructureShortcuts_DryRun covers all 8 shortcuts in
// lark_sheet_sheet_structure (sheet-info + 7 dim-*) and verifies the
// CLI 0-based exclusive-end → tool 1-based inclusive A1 conversion.
func TestSheetStructureShortcuts_DryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sc        common.Shortcut
		args      []string
		toolName  string
		wantInput map[string]interface{}
	}{
		{
			name:     "+sheet-info with include single category → narrow info_type",
			sc:       SheetInfo,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--include", "row_heights,col_widths"},
			toolName: "get_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"sheet_id":  testSheetID,
				"info_type": "row_heights_column_widths",
			},
		},
		{
			name:     "+sheet-info with mixed include → all",
			sc:       SheetInfo,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--include", "row_heights,merges"},
			toolName: "get_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"sheet_id":  testSheetID,
				"info_type": "all",
			},
		},
		{
			name:     "+dim-insert row 5..8 inherit-before → position 6 + count 3 + side",
			sc:       DimInsert,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "row", "--start", "5", "--end", "8", "--inherit-style", "before"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"operation": "insert",
				"sheet_id":  testSheetID,
				"position":  "6",
				"count":     float64(3),
				"side":      "before",
			},
		},
		{
			name:     "+dim-delete column B..D",
			sc:       DimDelete,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "column", "--start", "1", "--end", "4"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"operation": "delete",
				"sheet_id":  testSheetID,
				"range":     "B:D",
			},
		},
		{
			name:     "+dim-hide row 2..5 → range 3:5",
			sc:       DimHide,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "row", "--start", "2", "--end", "5"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"operation": "hide",
				"sheet_id":  testSheetID,
				"range":     "3:5",
			},
		},
		{
			name:     "+dim-unhide column 26..29 → AA:AC",
			sc:       DimUnhide,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "column", "--start", "26", "--end", "29"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"operation": "unhide",
				"sheet_id":  testSheetID,
				"range":     "AA:AC",
			},
		},
		{
			name:     "+dim-freeze row count=2 → freeze_rows",
			sc:       DimFreeze,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "row", "--count", "2"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":    testToken,
				"operation":   "freeze",
				"sheet_id":    testSheetID,
				"freeze_rows": float64(2),
			},
		},
		{
			name:     "+dim-freeze count=0 → unfreeze",
			sc:       DimFreeze,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "column", "--count", "0"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"operation": "unfreeze",
				"sheet_id":  testSheetID,
			},
		},
		{
			name:     "+dim-group with state",
			sc:       DimGroup,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "row", "--start", "0", "--end", "5", "--group-state", "fold"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":    testToken,
				"operation":   "group",
				"sheet_id":    testSheetID,
				"range":       "1:5",
				"group_state": "fold",
			},
		},
		{
			name:     "+dim-ungroup",
			sc:       DimUngroup,
			args:     []string{"--url", testURL, "--sheet-id", testSheetID, "--dimension", "row", "--start", "0", "--end", "5"},
			toolName: "modify_sheet_structure",
			wantInput: map[string]interface{}{
				"excel_id":  testToken,
				"operation": "ungroup",
				"sheet_id":  testSheetID,
				"range":     "1:5",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := parseDryRunBody(t, tt.sc, tt.args)
			got := decodeToolInput(t, body, tt.toolName)
			assertInputEquals(t, got, tt.wantInput)
		})
	}
}

func TestDimRange_StartEndValidation(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runShortcutCapturingErr(t, DimHide, []string{
		"--url", testURL, "--sheet-id", testSheetID,
		"--dimension", "row", "--start", "5", "--end", "3", "--dry-run",
	})
	if err == nil {
		t.Fatalf("expected validation error; stdout=%s stderr=%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr+err.Error(), "must be greater than --start") {
		t.Errorf("expected end>start guard; got=%s|%s|%v", stdout, stderr, err)
	}
}

// TestDimMove_DryRun verifies the legacy v2 dimension_range payload
// shape. CLI's 0-based inclusive (--start / --end) becomes the v2
// endpoint's half-open [startIndex, endIndex).
func TestDimMove_DryRun(t *testing.T) {
	t.Parallel()
	calls := parseDryRunAPI(t, DimMove, []string{
		"--url", testURL, "--sheet-id", testSheetID,
		"--dimension", "row", "--start", "0", "--end", "2", "--target", "10",
	})
	if len(calls) != 1 {
		t.Fatalf("api calls = %d, want 1", len(calls))
	}
	c := calls[0].(map[string]interface{})
	if !strings.Contains(c["url"].(string), "/sheets/v2/spreadsheets/") {
		t.Errorf("url = %v, want sheets/v2 path", c["url"])
	}
	body, _ := c["body"].(map[string]interface{})
	src, _ := body["source"].(map[string]interface{})
	if src["sheetId"] != testSheetID {
		t.Errorf("source.sheetId = %v", src["sheetId"])
	}
	if src["majorDimension"] != "ROWS" {
		t.Errorf("source.majorDimension = %v, want ROWS", src["majorDimension"])
	}
	if src["startIndex"].(float64) != 0 {
		t.Errorf("startIndex = %v, want 0", src["startIndex"])
	}
	if src["endIndex"].(float64) != 3 {
		t.Errorf("endIndex = %v, want 3 (CLI end+1 for half-open)", src["endIndex"])
	}
	if body["destinationIndex"].(float64) != 10 {
		t.Errorf("destinationIndex = %v, want 10", body["destinationIndex"])
	}
}

// TestColumnIndexToLetter exercises the corner cases of the letter helper:
// single, double, and triple-letter spans.
func TestColumnIndexToLetter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		idx  int
		want string
	}{
		{0, "A"}, {25, "Z"}, {26, "AA"}, {27, "AB"}, {51, "AZ"},
		{52, "BA"}, {701, "ZZ"}, {702, "AAA"},
	}
	for _, c := range cases {
		if got := columnIndexToLetter(c.idx); got != c.want {
			t.Errorf("columnIndexToLetter(%d) = %q, want %q", c.idx, got, c.want)
		}
	}
}
