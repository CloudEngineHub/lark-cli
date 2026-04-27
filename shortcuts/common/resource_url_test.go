// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package common

import (
	"testing"

	"github.com/larksuite/cli/internal/core"
)

func TestBuildResourceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		brand core.LarkBrand
		kind  string
		token string
		want  string
	}{
		{"feishu docx", core.BrandFeishu, "docx", "doxcnABC", "https://www.feishu.cn/docx/doxcnABC"},
		{"feishu doc legacy", core.BrandFeishu, "doc", "doccnABC", "https://www.feishu.cn/docs/doccnABC"},
		{"feishu sheet", core.BrandFeishu, "sheet", "shtcnABC", "https://www.feishu.cn/sheets/shtcnABC"},
		{"feishu bitable", core.BrandFeishu, "bitable", "bascnABC", "https://www.feishu.cn/base/bascnABC"},
		{"feishu wiki", core.BrandFeishu, "wiki", "wikcnABC", "https://www.feishu.cn/wiki/wikcnABC"},
		{"feishu file", core.BrandFeishu, "file", "boxcnABC", "https://www.feishu.cn/file/boxcnABC"},
		{"feishu folder", core.BrandFeishu, "folder", "fldcnABC", "https://www.feishu.cn/drive/folder/fldcnABC"},
		{"lark docx", core.BrandLark, "docx", "doxcnABC", "https://www.larksuite.com/docx/doxcnABC"},
		{"lark wiki", core.BrandLark, "wiki", "wikcnABC", "https://www.larksuite.com/wiki/wikcnABC"},
		{"empty brand defaults to feishu", core.LarkBrand(""), "docx", "doxcnABC", "https://www.feishu.cn/docx/doxcnABC"},
		{"kind case-insensitive", core.BrandFeishu, "DOCX", "doxcnABC", "https://www.feishu.cn/docx/doxcnABC"},
		{"token whitespace trimmed", core.BrandFeishu, "docx", "  doxcnABC  ", "https://www.feishu.cn/docx/doxcnABC"},
		{"empty token", core.BrandFeishu, "docx", "", ""},
		{"whitespace-only token", core.BrandFeishu, "docx", "   ", ""},
		{"unknown kind", core.BrandFeishu, "mindnote", "mncnABC", ""},
		{"empty kind", core.BrandFeishu, "", "doxcnABC", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildResourceURL(tt.brand, tt.kind, tt.token)
			if got != tt.want {
				t.Errorf("BuildResourceURL(%q, %q, %q) = %q, want %q", tt.brand, tt.kind, tt.token, got, tt.want)
			}
		})
	}
}
