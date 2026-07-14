package report

import (
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// trial1 は、候補パターンを当てた結果。2件のうち1件が、折り返しの継ぎ目に左右される当たり。
func trial1() *audit.Trial {
	return &audit.Trial{
		Pattern:  "以前は",
		Files:    4,
		Comments: 50,
		Hits: []audit.Hit{
			{
				Path: "internal/store/index.go",
				Body: "以前はここで畳んでいた。",
				Comment: place.Comment{
					Kind: scan.KindLine, Place: place.Leading, Line: 8, Col: 2,
					Text: "// 以前はここで畳んでいた。",
				},
			},
			{
				Path:          "internal/store/tree.go",
				Body:          "この鍵は 以前は 32 バイトだった。",
				SeamDependent: true,
				Comment: place.Comment{
					Kind: scan.KindDocLine, Place: place.Doc, Line: 20, Col: 1,
					Text: "// この鍵は\n// 以前は 32 バイトだった。",
				},
			},
		},
	}
}

// TestTryText は、当たりを全部出すこと、継ぎ目に左右される当たりの見出しに印が付くことを見る
// （測っている最中に、どの当たりが継ぎ目のせいかを見分けられるように）。
func TestTryText(t *testing.T) {
	var b strings.Builder
	if err := TryText(&b, trial1()); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `internal/store/index.go:8:2  place=leading kind=line
  // 以前はここで畳んでいた。

internal/store/tree.go:20:1  place=doc kind=docline  seam=dependent
  // この鍵は
  // 以前は 32 バイトだった。

以前は に 2 件が当たりました（4 ファイル / 50 コメント中 4.00%）
真陽性か偽陽性かは、esorp は判定しません。当たりを読んで、足すかどうかを決めてください。
`)
}

// TestTryTextNoComments は、母集団が空でも割り算で落ちないことを見る。
func TestTryTextNoComments(t *testing.T) {
	var b strings.Builder
	if err := TryText(&b, &audit.Trial{Pattern: "以前は"}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `以前は に 0 件が当たりました（0 ファイル / 0 コメント中 0%）
真陽性か偽陽性かは、esorp は判定しません。当たりを読んで、足すかどうかを決めてください。
`)
}

// TestTryJSON は、照合に使った本文（body）が出ること、seam_dependent が立ったときだけ出ることを
// 見る（原文だけでは、句が行をまたいで当たったときに、なぜ当たったのかが読み取れない）。
func TestTryJSON(t *testing.T) {
	var b strings.Builder
	if err := TryJSON(&b, trial1()); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 1,
  "pattern": "以前は",
  "summary": {
    "files": 4,
    "comments": 50,
    "hits": 2
  },
  "hits": [
    {
      "path": "internal/store/index.go",
      "line": 8,
      "col": 2,
      "place": "leading",
      "kind": "line",
      "text": "// 以前はここで畳んでいた。",
      "body": "以前はここで畳んでいた。"
    },
    {
      "path": "internal/store/tree.go",
      "line": 20,
      "col": 1,
      "place": "doc",
      "kind": "docline",
      "text": "// この鍵は\n// 以前は 32 バイトだった。",
      "body": "この鍵は 以前は 32 バイトだった。",
      "seam_dependent": true
    }
  ],
  "skipped": []
}
`)
}
