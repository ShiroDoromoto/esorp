package report

import (
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// bodyViolation は、取り出しの要らない入力の違反1件。器も種別も持たない（place.None / scan.KindNone）。
func bodyViolation() rule.Violation {
	return rule.Violation{
		ID:       "no-history",
		Line:     3,
		Severity: config.SeverityEnforce,
		Place:    place.None,
		Kind:     scan.KindNone,
		Text:     "この関数の同期版は削除ずみ。",
		Message:  "変化を語っています。今の姿だけを書いてください。",
		Site:     rule.Site{Path: "rules[0]", Allow: -1, Rule: 0},
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

1 violations
Only layer 2 (lexicon) applied. Layer 1 (vessel and form) does not apply (the body passed in has no vessel).
`)
}

// TestBodyTextClean は、違反が無くても、当たらない層を告げることを見る。黙って通すと、通ったことが
// 「層1 も通った」と読まれる。
func TestBodyTextClean(t *testing.T) {
	var b strings.Builder
	if err := BodyText(&b, nil); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `esorp: no violations
Only layer 2 (lexicon) applied. Layer 1 (vessel and form) does not apply (the body passed in has no vessel).
`)
}

func TestBodyJSON(t *testing.T) {
	var b strings.Builder
	if err := BodyJSON(&b, []rule.Violation{bodyViolation()}); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 2,
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
  "summary": {
    "violations": 1,
    "enforce": 1,
    "advisory": 0
  },
  "violations": [
    {
      "line": 3,
      "id": "no-history",
      "severity": "enforce",
      "text": "この関数の同期版は削除ずみ。",
      "message": "変化を語っています。今の姿だけを書いてください。"
    }
  ]
}
`)
}

// TestBodyAdvisory は、advisory の違反が「弱い」と分かる形で出ることを見る。人間向けの出力では
// 違反 id に印が付き、集計に内訳が出る。機械向けでは severity の欄がそのまま値を持つ。
func TestBodyAdvisory(t *testing.T) {
	v := bodyViolation()
	v.Severity = config.SeverityAdvisory

	var text strings.Builder
	if err := BodyText(&text, []rule.Violation{v}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3  no-history  [advisory]", "1 violations (0 enforce / 1 advisory)"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, text.String())
		}
	}

	var b strings.Builder
	if err := BodyJSON(&b, []rule.Violation{v}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"severity": "advisory"`, `"enforce": 0`, `"advisory": 1`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, b.String())
		}
	}
}

// TestBodyJSONEmpty は、違反が無くても violations が空配列であることと、当たらない層が出ることを見る。
func TestBodyJSONEmpty(t *testing.T) {
	var b strings.Builder
	if err := BodyJSON(&b, nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"violations": []`, `"not_applied"`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, b.String())
		}
	}
}
