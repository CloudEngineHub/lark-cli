// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package sheets contains lark-sheets shortcuts aligned with the
// sheet-skill-spec canonical layout. Each shortcut wraps a single
// sheet-ai-skills tool behind the One-OpenAPI endpoint
// (sheet_ai/v2/.../tools/invoke_{read,write}).
package sheets

import (
	"strings"

	"github.com/larksuite/cli/internal/validate"
	"github.com/larksuite/cli/shortcuts/common"
)

// resolveSpreadsheetToken applies the public --url / --spreadsheet-token XOR
// pair shared by every sheets canonical shortcut and returns the resolved
// token. Network-free, safe to call from Validate and DryRun.
func resolveSpreadsheetToken(runtime *common.RuntimeContext) (string, error) {
	if err := common.ExactlyOne(runtime, "url", "spreadsheet-token"); err != nil {
		return "", err
	}
	if token := strings.TrimSpace(runtime.Str("spreadsheet-token")); token != "" {
		if err := validate.RejectControlChars(token, "spreadsheet-token"); err != nil {
			return "", common.FlagErrorf("%v", err)
		}
		return token, nil
	}

	url := strings.TrimSpace(runtime.Str("url"))
	token := extractSpreadsheetToken(url)
	if token == "" || token == url {
		return "", common.FlagErrorf("--url must be a spreadsheet URL like https://.../sheets/<token>")
	}
	if err := validate.RejectControlChars(token, "url"); err != nil {
		return "", common.FlagErrorf("%v", err)
	}
	return token, nil
}

// extractSpreadsheetToken pulls the token segment out of a /sheets/<token>
// or /spreadsheets/<token> URL. Returns the input unchanged when no known
// prefix is present (callers must check token != originalInput).
func extractSpreadsheetToken(input string) string {
	input = strings.TrimSpace(input)
	for _, prefix := range []string{"/sheets/", "/spreadsheets/"} {
		if idx := strings.Index(input, prefix); idx >= 0 {
			token := input[idx+len(prefix):]
			if idx2 := strings.IndexAny(token, "/?#"); idx2 >= 0 {
				token = token[:idx2]
			}
			return token
		}
	}
	return input
}

// publicTokenFlags is the leading pair of every canonical sheets shortcut.
// Shortcuts targeting a single sheet append the public sheet-id / sheet-name
// XOR pair on top of this; workbook-level shortcuts use this pair only.
func publicTokenFlags() []common.Flag {
	return []common.Flag{
		{Name: "url", Desc: "spreadsheet URL (XOR --spreadsheet-token)"},
		{Name: "spreadsheet-token", Desc: "spreadsheet token (XOR --url)"},
	}
}
