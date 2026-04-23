// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package doc

import (
	"strings"
	"testing"
)

func TestComputeMarkdownDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		before          string
		after           string
		wantEmpty       bool
		wantContainsAll []string
		wantLacks       []string
	}{
		{
			name:      "identical inputs yield empty diff",
			before:    "line1\nline2\nline3",
			after:     "line1\nline2\nline3",
			wantEmpty: true,
		},
		{
			name:   "single-line replacement in the middle",
			before: "header\n\noriginal paragraph\n\nfooter",
			after:  "header\n\nreplacement paragraph\n\nfooter",
			wantContainsAll: []string{
				"-original paragraph",
				"+replacement paragraph",
				"@@ ",
				" header",
				" footer",
			},
		},
		{
			name:   "pure insertion keeps removed section empty",
			before: "start\nend",
			after:  "start\nnew middle line\nend",
			wantContainsAll: []string{
				"+new middle line",
				" start",
				" end",
			},
		},
		{
			name:   "pure deletion keeps added section empty",
			before: "start\nstale middle\nend",
			after:  "start\nend",
			wantContainsAll: []string{
				"-stale middle",
				" start",
				" end",
			},
		},
		{
			name:   "prepend at the start has no leading context",
			before: "first\nsecond",
			after:  "brand new header\nfirst\nsecond",
			wantContainsAll: []string{
				"+brand new header",
				" first",
				" second",
			},
		},
		{
			name:   "append at the end has no trailing context",
			before: "first\nsecond",
			after:  "first\nsecond\ntrailer",
			wantContainsAll: []string{
				"+trailer",
				" first",
				" second",
			},
		},
		{
			name:      "empty-to-empty yields empty diff",
			before:    "",
			after:     "",
			wantEmpty: true,
		},
		{
			// Regression: strings.Split("", "\n") returns [""], which would
			// have produced a spurious "-\n" blank-line deletion on empty→content.
			name:            "empty before to non-empty after has only additions",
			before:          "",
			after:           "new content",
			wantContainsAll: []string{"+new content"},
			wantLacks:       []string{"-\n"},
		},
		{
			name:            "non-empty before to empty after has only deletions",
			before:          "old content",
			after:           "",
			wantContainsAll: []string{"-old content"},
			wantLacks:       []string{"+\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := computeMarkdownDiff(tt.before, tt.after)
			if tt.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty diff, got: %q", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("expected non-empty diff, got empty")
			}
			for _, needle := range tt.wantContainsAll {
				if !strings.Contains(got, needle) {
					t.Errorf("diff missing expected substring %q; full diff:\n%s", needle, got)
				}
			}
			for _, forbidden := range tt.wantLacks {
				if strings.Contains(got, forbidden) {
					t.Errorf("diff unexpectedly contains %q; full diff:\n%s", forbidden, got)
				}
			}
		})
	}
}

func TestComputeMarkdownDiffHunkHeaderZeroLengthRange(t *testing.T) {
	t.Parallel()

	// Empty before: unified-diff convention is start=0 for a zero-length
	// range, not start=1.
	got := computeMarkdownDiff("", "hello\nworld")
	if !strings.Contains(got, "@@ -0,0 ") {
		t.Errorf("expected zero-length before range as '-0,0', got:\n%s", got)
	}

	// Empty after: symmetric case.
	got = computeMarkdownDiff("hello\nworld", "")
	if !strings.Contains(got, " +0,0 @@") {
		t.Errorf("expected zero-length after range as '+0,0', got:\n%s", got)
	}
}

func TestSplitDiffLinesStripsTrailingNewline(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"":           nil,
		"a":          {"a"},
		"a\n":        {"a"},
		"a\nb":       {"a", "b"},
		"a\nb\n":     {"a", "b"},
		"a\n\nb\n":   {"a", "", "b"},
		"\n":         {""}, // a single blank line should still be preserved
		"only-blank": {"only-blank"},
	}
	for input, want := range cases {
		got := splitDiffLines(input)
		if len(got) != len(want) {
			t.Errorf("splitDiffLines(%q) = %q, want %q", input, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitDiffLines(%q)[%d] = %q, want %q", input, i, got[i], want[i])
			}
		}
	}
}

// TestComputeMarkdownDiffTrailingNewlineParity confirms a single-line
// replacement against snapshots ending in '\n' (what fetch-doc returns)
// produces the same diff as snapshots without the trailing newline.
// Previously the trailing "" generated a phantom blank context line.
func TestComputeMarkdownDiffTrailingNewlineParity(t *testing.T) {
	t.Parallel()

	withNL := computeMarkdownDiff("a\nold\nb\n", "a\nnew\nb\n")
	withoutNL := computeMarkdownDiff("a\nold\nb", "a\nnew\nb")
	if withNL != withoutNL {
		t.Fatalf("diff should not depend on trailing newline\nwith:\n%s\n\nwithout:\n%s", withNL, withoutNL)
	}
}

func TestComputeMarkdownDiffHunkHeaderLineNumbers(t *testing.T) {
	t.Parallel()

	before := "l1\nl2\nl3\nl4\nl5\nl6"
	after := "l1\nl2\nl3\nCHANGED\nl5\nl6"

	got := computeMarkdownDiff(before, after)

	// Context is capped at 2 lines on each side, so the hunk should start
	// at line 2 (= line 4 - 2 context) and span 5 lines before / 5 after.
	wantHeader := "@@ -2,5 +2,5 @@"
	if !strings.Contains(got, wantHeader) {
		t.Fatalf("expected hunk header %q; got diff:\n%s", wantHeader, got)
	}
}
