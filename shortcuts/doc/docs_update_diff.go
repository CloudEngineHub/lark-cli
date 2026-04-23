// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package doc

import (
	"fmt"
	"strings"

	"github.com/larksuite/cli/shortcuts/common"
)

// diffContextLines is the number of unchanged lines printed on either side of
// each diff hunk. Matches the `git diff -U2` convention, which is enough to
// orient a reader in most docx blocks without drowning stderr in boilerplate.
const diffContextLines = 2

// computeMarkdownDiff returns a git-style unified diff between before and
// after, focused on the single changed region between the longest common
// prefix and the longest common suffix. Returns an empty string when before
// and after are identical.
//
// The algorithm is intentionally simple — not Myers, not minimal — because
// `docs +update` replace/insert/delete modes touch a localized block range,
// so the "middle" that survives prefix/suffix trimming is already the
// user-visible change. A full LCS diff would buy better output for paired
// additions+deletions but at several hundred lines of implementation we
// don't need right now.
func computeMarkdownDiff(before, after string) string {
	if before == after {
		return ""
	}
	beforeLines := splitDiffLines(before)
	afterLines := splitDiffLines(after)

	// Longest common prefix.
	prefix := 0
	for prefix < len(beforeLines) && prefix < len(afterLines) &&
		beforeLines[prefix] == afterLines[prefix] {
		prefix++
	}

	// Longest common suffix, not overlapping the prefix on either side.
	suffix := 0
	for suffix < len(beforeLines)-prefix &&
		suffix < len(afterLines)-prefix &&
		beforeLines[len(beforeLines)-1-suffix] == afterLines[len(afterLines)-1-suffix] {
		suffix++
	}

	beforeEnd := len(beforeLines) - suffix
	afterEnd := len(afterLines) - suffix

	ctxStart := prefix - diffContextLines
	if ctxStart < 0 {
		ctxStart = 0
	}
	ctxEndBefore := beforeEnd + diffContextLines
	if ctxEndBefore > len(beforeLines) {
		ctxEndBefore = len(beforeLines)
	}
	ctxEndAfter := afterEnd + diffContextLines
	if ctxEndAfter > len(afterLines) {
		ctxEndAfter = len(afterLines)
	}

	var sb strings.Builder
	// Hunk header uses 1-based line numbers matching unified-diff
	// convention, except that a zero-length range uses start=0 (git diff /
	// diff -u do the same). Without this, a pure insertion into an empty
	// "before" snapshot prints `@@ -1,0 +1,N @@` which is technically
	// malformed.
	beforeCount := ctxEndBefore - ctxStart
	afterCount := ctxEndAfter - ctxStart
	beforeStart := ctxStart + 1
	if beforeCount == 0 {
		beforeStart = 0
	}
	afterStart := ctxStart + 1
	if afterCount == 0 {
		afterStart = 0
	}
	fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n",
		beforeStart, beforeCount,
		afterStart, afterCount,
	)
	for i := ctxStart; i < prefix; i++ {
		fmt.Fprintf(&sb, " %s\n", beforeLines[i])
	}
	for i := prefix; i < beforeEnd; i++ {
		fmt.Fprintf(&sb, "-%s\n", beforeLines[i])
	}
	for i := prefix; i < afterEnd; i++ {
		fmt.Fprintf(&sb, "+%s\n", afterLines[i])
	}
	for i := beforeEnd; i < ctxEndBefore; i++ {
		fmt.Fprintf(&sb, " %s\n", beforeLines[i])
	}
	return sb.String()
}

// splitDiffLines returns the logical content lines of s for diff purposes.
// It differs from strings.Split in two ways:
//
//  1. The empty input maps to a zero-length slice instead of [""], so an
//     empty-vs-content diff doesn't emit a spurious blank-line add/remove.
//  2. A single trailing "\n" is treated as a line terminator, not an extra
//     blank line — so "a\nb\n" splits to ["a", "b"] rather than ["a", "b", ""].
//     Without this, snapshots from fetch-doc (which always end with a
//     newline) would report a phantom blank context line and inflate hunk
//     counts.
func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// fetchMarkdownForDiff calls the fetch-doc MCP tool and extracts the
// markdown payload. Errors are returned verbatim so the caller can decide
// whether to block the update on a failing snapshot (currently: no — the
// update still proceeds and the diff section is skipped).
//
// A paginated response (has_more=true) is reported as an error instead of
// silently returning the first page: diffing a partial snapshot produces
// misleading output (edits outside the fetched range look like "no textual
// change"), and the right fix — paginating all the way — is a bigger
// investment than the --show-diff surface currently warrants.
func fetchMarkdownForDiff(runtime *common.RuntimeContext, docID string) (string, error) {
	result, err := common.CallMCPTool(runtime, "fetch-doc", map[string]interface{}{
		"doc_id":           docID,
		"skip_task_detail": true,
	})
	if err != nil {
		return "", err
	}
	if hasMore, ok := result["has_more"].(bool); ok && hasMore {
		return "", fmt.Errorf("fetch-doc returned a paginated snapshot (has_more=true); --show-diff cannot diff partial documents")
	}
	md, _ := result["markdown"].(string)
	return md, nil
}
