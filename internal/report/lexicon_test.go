package report

import (
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// trial1 は、候補パターンを当てた結果。2件のうち1件が、折り返しの継ぎ目に左右される当たり。面は2つ
// あり、当たりは cstyle 面に偏っている（全体の件数だけを見せると、この偏りが平均に埋もれる）。
func trial1() *audit.Trial {
	return &audit.Trial{
		Pattern:  "以前は",
		Files:    4,
		Comments: 50,
		Surfaces: []audit.Surface{
			{Syntax: "cstyle", Files: 3, Comments: 40, Hits: 2},
			{Syntax: "hash", Files: 1, Comments: 10, Hits: 0},
		},
		Hits: []audit.Hit{
			{
				Path:   "internal/store/index.go",
				Syntax: "cstyle",
				Body:   "以前はここで畳んでいた。",
				Comment: place.Comment{
					Kind: scan.KindLine, Place: place.Leading, Line: 8, Col: 2,
					Text: "// 以前はここで畳んでいた。",
				},
			},
			{
				Path:          "internal/store/tree.go",
				Syntax:        "cstyle",
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

// TestTryText は、当たりを全部出すこと、面ごとの内訳が出ること、継ぎ目に左右される当たりの見出しに
// 印が付くことを見る（測っている最中に、どの当たりが継ぎ目のせいかを見分けられるように）。
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

面ごとの内訳:
  cstyle              2 件 / 40 コメント（5.00%）
  hash                0 件 / 10 コメント（0.00%）

text 面（check --text -）は測れません。渡される本文はツリーの外にあり、当てるコーパスがありません。
この面に当てるルールは、当たりを見て決めてください（0 件と出しているのではなく、測っていません）。
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

text 面（check --text -）は測れません。渡される本文はツリーの外にあり、当てるコーパスがありません。
この面に当てるルールは、当たりを見て決めてください（0 件と出しているのではなく、測っていません）。
真陽性か偽陽性かは、esorp は判定しません。当たりを読んで、足すかどうかを決めてください。
`)
}

// TestTryJSON は、照合に使った本文（body）が出ること、面ごとの内訳と、text 面を測っていないことが
// 出ること、seam_dependent が立ったときだけ出ることを見る。
func TestTryJSON(t *testing.T) {
	var b strings.Builder
	if err := TryJSON(&b, trial1()); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 2,
  "pattern": "以前は",
  "summary": {
    "files": 4,
    "comments": 50,
    "hits": 2
  },
  "surfaces": [
    {
      "syntax": "cstyle",
      "files": 3,
      "comments": 40,
      "hits": 2
    },
    {
      "syntax": "hash",
      "files": 1,
      "comments": 10,
      "hits": 0
    }
  ],
  "text_surface": {
    "measured": false,
    "reason": "text 面（check --text -）は測れません。渡される本文はツリーの外にあり、当てるコーパスがありません。\nこの面に当てるルールは、当たりを見て決めてください（0 件と出しているのではなく、測っていません）。"
  },
  "hits": [
    {
      "path": "internal/store/index.go",
      "syntax": "cstyle",
      "line": 8,
      "col": 2,
      "place": "leading",
      "kind": "line",
      "text": "// 以前はここで畳んでいた。",
      "body": "以前はここで畳んでいた。"
    },
    {
      "path": "internal/store/tree.go",
      "syntax": "cstyle",
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
