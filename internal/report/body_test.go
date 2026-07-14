package report

import (
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// bodyViolation は、取り出しの要らない入力の違反1件。器も種別も持たない（place.None / scan.KindNone）。
func bodyViolation() rule.Violation {
	return rule.Violation{
		ID:      "no-history",
		Line:    3,
		Place:   place.None,
		Kind:    scan.KindNone,
		Text:    "この関数の同期版は削除ずみ。",
		Message: "変化を語っています。今の姿だけを書いてください。",
		Site:    rule.Site{Path: "rules[0]", Allow: -1, Rule: 0},
	}
}

func TestBodyText(t *testing.T) {
	var b strings.Builder
	if err := BodyText(&b, []rule.Violation{bodyViolation()}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `3  no-history
  この関数の同期版は削除ずみ。
  変化を語っています。今の姿だけを書いてください。

1 件の違反
当たったのは層2（語彙）だけです。層1（器・書式）は当たりません（渡された本文は器を持ちません）。
baseline はありません（その場限りの入力なので、抑制のキーが立ちません）。
`)
}

// TestBodyTextClean は、違反が無くても、当たらない層を告げることを見る。黙って通すと、通ったことが
// 「層1 も通った」と読まれる。
func TestBodyTextClean(t *testing.T) {
	var b strings.Builder
	if err := BodyText(&b, nil); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `esorp: 違反はありません
当たったのは層2（語彙）だけです。層1（器・書式）は当たりません（渡された本文は器を持ちません）。
baseline はありません（その場限りの入力なので、抑制のキーが立ちません）。
`)
}

func TestBodyJSON(t *testing.T) {
	var b strings.Builder
	if err := BodyJSON(&b, []rule.Violation{bodyViolation()}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 1,
  "surface": "text",
  "layers": {
    "applied": [
      "lexicon"
    ],
    "not_applied": [
      "vessel",
      "form"
    ]
  },
  "baseline": false,
  "summary": {
    "violations": 1
  },
  "violations": [
    {
      "line": 3,
      "id": "no-history",
      "text": "この関数の同期版は削除ずみ。",
      "message": "変化を語っています。今の姿だけを書いてください。"
    }
  ]
}
`)
}

// TestBodyJSONEmpty は、違反が無くても violations が空配列であることと、当たらない層が出ることを見る。
func TestBodyJSONEmpty(t *testing.T) {
	var b strings.Builder
	if err := BodyJSON(&b, nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"violations": []`, `"not_applied"`, `"baseline": false`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, b.String())
		}
	}
}
