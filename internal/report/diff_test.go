package report

import (
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
)

// TestDiffJSON は、差分がキーと値に割れて出ることを見る（読むのは人とはかぎらないので、組み立て
// 済みの1行だけにしない）。
func TestDiffJSON(t *testing.T) {
	sections := []config.Section{{
		Title: "syntax.cstyle",
		Changes: []config.Change{
			{Key: "syntax.cstyle.allow[doc].kind", Local: "[docline]", Tmpl: "[docline, docblock]", Text: "kind が違います"},
			{Key: "rules[no-history]", Only: "template", Text: "テンプレートにだけあります"},
		},
	}}

	var b strings.Builder
	if err := DiffJSON(&b, "esorp.yaml", sections); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 1,
  "config": "esorp.yaml",
  "same": false,
  "sections": [
    {
      "title": "syntax.cstyle",
      "changes": [
        {
          "key": "syntax.cstyle.allow[doc].kind",
          "local": "[docline]",
          "template": "[docline, docblock]",
          "text": "kind が違います"
        },
        {
          "key": "rules[no-history]",
          "only": "template",
          "text": "テンプレートにだけあります"
        }
      ]
    }
  ]
}
`)
}

// TestDiffJSONSame は、差分が無ければ same が立ち、sections が空配列で出ることを見る。
func TestDiffJSONSame(t *testing.T) {
	var b strings.Builder
	if err := DiffJSON(&b, "esorp.yaml", nil); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 1,
  "config": "esorp.yaml",
  "same": true,
  "sections": []
}
`)
}
