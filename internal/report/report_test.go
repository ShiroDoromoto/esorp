package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// vessel1 は、器に反した違反1件（層1）。Site.Rule が -1 なのは、層2 のルールに当たっていないこと。
func vessel1() audit.Finding {
	return audit.Finding{
		Path:   "internal/store/index.go",
		Syntax: "cstyle",
		Violation: rule.Violation{
			ID:       rule.PlaceNotAllowed,
			Line:     8,
			Col:      2,
			Severity: config.SeverityEnforce,
			Place:    place.Leading,
			Kind:     scan.KindLine,
			Text:     "// 前方移行はここで行っていた。",
			Message:  "この位置のコメントは許可されていません。\n",
			Site:     rule.Site{Path: "syntax.cstyle.allow", Rule: -1},
		},
	}
}

// lexicon1 は、語彙に当たった違反1件（層2）。折り返しの継ぎ目に左右される当たり。
func lexicon1() audit.Finding {
	return audit.Finding{
		Path:   "internal/store/index.go",
		Syntax: "cstyle",
		Violation: rule.Violation{
			ID:            "no-history",
			Line:          20,
			Col:           1,
			Severity:      config.SeverityEnforce,
			Place:         place.Doc,
			Kind:          scan.KindDocLine,
			Text:          "// 以前はここで畳んでいた。",
			Message:       "履歴を書かないでください。",
			Site:          rule.Site{Path: "rules[0]", Rule: 0},
			SeamDependent: true,
		},
	}
}

// wants は、出力の全文が期待どおりであることを見る。形そのものが契約なので、部分一致で見ない。
func wants(t *testing.T, got, want string) {
	t.Helper()

	if got != want {
		t.Fatalf("出力が違います:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// decode は、書き出した JSON を読み直す。
func decode(t *testing.T, s string, v any) {
	t.Helper()

	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("JSON を読めません: %v\n%s", err, s)
	}
}

func TestTextNoFindings(t *testing.T) {
	var b strings.Builder
	if err := Text(&b, &audit.Result{Files: 3, Comments: 12}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), "esorp: no violations (3 files / 12 comments)\n")
}

// TestText は、1件が「どこで・何に反し・何が書かれていて・どう始末するか」で閉じていること、
// 継ぎ目に左右される当たりにだけ断りが添うことを見る。
func TestText(t *testing.T) {
	res := &audit.Result{
		Files:    3,
		Comments: 12,
		Findings: []audit.Finding{vessel1(), lexicon1()},
	}

	var b strings.Builder
	if err := Text(&b, res); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `internal/store/index.go:8:2  place-not-allowed  place=leading kind=line
  // 前方移行はここで行っていた。
  この位置のコメントは許可されていません。

internal/store/index.go:20:1  no-history  place=doc kind=docline
  // 以前はここで畳んでいた。
  履歴を書かないでください。
  This match depends on a line-wrap seam. The line wrapped at the boundary between half-width and full-width characters,
  and whether whitespace stood there in the original cannot be recovered. If there is nothing to fix in the original, narrow the rule
  with where.path, or put its id on severity: advisory.

2 violations (3 files / 12 comments)
`)
}

// TestTextAdvisory は、advisory の違反が「弱い」と分かる形で出ることを見る。印が付くのは advisory
// だけで、集計の内訳は advisory が居るときだけ出る（severity: を書いていないプロジェクトの出力は
// 今までと同じ形のまま）。
func TestTextAdvisory(t *testing.T) {
	weak := lexicon1()
	weak.Severity = config.SeverityAdvisory

	var b strings.Builder
	if err := Text(&b, &audit.Result{Files: 1, Comments: 2, Findings: []audit.Finding{vessel1(), weak}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"place-not-allowed  place=leading",
		"no-history  [advisory]  place=doc",
		"2 violations (1 enforce / 1 advisory) (1 files / 2 comments)",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, b.String())
		}
	}
}

// TestTextWithoutMessage は、disposition が空のときに空行を差し込まないことを見る（省略できる）。
func TestTextWithoutMessage(t *testing.T) {
	f := vessel1()
	f.Message = ""

	var b strings.Builder
	if err := Text(&b, &audit.Result{Files: 1, Comments: 1, Findings: []audit.Finding{f}}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `internal/store/index.go:8:2  place-not-allowed  place=leading kind=line
  // 前方移行はここで行っていた。

1 violations (1 files / 1 comments)
`)
}

// TestJSON は、キーの並びを全文で見る。読むのは層3 のエージェントで、1つ変われば黙って壊れる。
// seam_dependent は立ったときだけ出る。
func TestJSON(t *testing.T) {
	res := &audit.Result{
		Files:    3,
		Comments: 12,
		Findings: []audit.Finding{vessel1(), lexicon1()},
		Skipped:  []string{"internal/store/index.rb"},
	}

	var b strings.Builder
	if err := JSON(&b, res); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 4,
  "summary": {
    "files": 3,
    "comments": 12,
    "violations": 2,
    "enforce": 2,
    "advisory": 0
  },
  "violations": [
    {
      "path": "internal/store/index.go",
      "line": 8,
      "col": 2,
      "id": "place-not-allowed",
      "severity": "enforce",
      "place": "leading",
      "kind": "line",
      "text": "// 前方移行はここで行っていた。",
      "message": "この位置のコメントは許可されていません。"
    },
    {
      "path": "internal/store/index.go",
      "line": 20,
      "col": 1,
      "id": "no-history",
      "severity": "enforce",
      "place": "doc",
      "kind": "docline",
      "text": "// 以前はここで畳んでいた。",
      "message": "履歴を書かないでください。",
      "seam_dependent": true
    }
  ],
  "skipped": [
    "internal/store/index.rb"
  ]
}
`)
}

// TestJSONEmptyArrays は、violations と skipped が空でも null でなく空配列で出ることを見る。
func TestJSONEmptyArrays(t *testing.T) {
	var b strings.Builder
	if err := JSON(&b, &audit.Result{Files: 1, Comments: 4}); err != nil {
		t.Fatal(err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"violations", "skipped"} {
		if string(got[key]) != "[]" {
			t.Errorf("%s = %s、空配列であるべきです", key, got[key])
		}
	}
	if _, ok := got["review"]; ok {
		t.Error("層3 を開いていないのに review が出ています")
	}
}

// TestJSONReview は、層3 を開いたときだけ review が出ること、その形を見る。
func TestJSONReview(t *testing.T) {
	res := &audit.Result{
		Files:    1,
		Comments: 1,
		Review: &audit.Review{
			Question: "このコメントは、コードから読み取れない事情を語っていますか。\n",
			Comments: []audit.Passed{{
				Path: "internal/store/index.go",
				Comment: place.Comment{
					Kind:  scan.KindDocLine,
					Place: place.Doc,
					Line:  4,
					Col:   1,
					Text:  "// Index は、鍵から位置を引く。",
				},
			}},
		},
	}

	var b strings.Builder
	if err := JSON(&b, res); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), `"review": {
    "question": "このコメントは、コードから読み取れない事情を語っていますか。",
    "comments": [
      {
        "path": "internal/store/index.go",
        "line": 4,
        "col": 1,
        "place": "doc",
        "kind": "docline",
        "text": "// Index は、鍵から位置を引く。"
      }
    ]
  }`) {
		t.Fatalf("review の形が違います:\n%s", b.String())
	}
}

func TestWarnings(t *testing.T) {
	var b strings.Builder
	if err := Warnings(&b, []string{"a.rb", "b.py"}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `esorp: 2 files were not inspected (no scanner for that language yet):
  a.rb
  b.py
`)
}

// TestWarningsNone は、検査できなかったファイルが無ければ何も書かないことを見る。
func TestWarningsNone(t *testing.T) {
	var b strings.Builder
	if err := Warnings(&b, nil); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), "")
}
