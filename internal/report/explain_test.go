package report

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/baseline"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/rule"
)

// cfg1 は、explain が根拠として読む設定。doc だけを許し、そこに書式（段落数）を課している。
func cfg1() *config.Config {
	paragraphs := 1
	return &config.Config{
		Syntax: map[string]config.Syntax{
			"cstyle": {Allow: []config.Allow{
				{Place: "header"},
				{
					Place: "doc",
					Kind:  []string{"docline"},
					Label: []string{"TODO:", "SAFETY:"},
					Form:  &config.Form{Subject: "declaration", Paragraphs: &paragraphs},
				},
			}},
		},
		Rules: []config.Rule{{
			ID:      "no-history",
			Pattern: "以前は|かつては",
			Message: "履歴を書かないでください。\n",
			Where:   config.Where{Syntax: []string{"cstyle"}, Kind: []string{"docline"}},
		}},
	}
}

// empty は、何も載っていない baseline。
func empty(t *testing.T) *baseline.Baseline {
	t.Helper()

	b, err := baseline.Load("")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// holds は、その違反1件だけを載せた baseline。keys は非公開なので、書いて読み直す。
func holds(t *testing.T, f audit.Finding) *baseline.Baseline {
	t.Helper()

	path := filepath.Join(t.TempDir(), "esorp-baseline.json")
	if err := baseline.Save(path, []baseline.Entry{{Key: f.Key, Path: f.Path, ID: f.ID}}); err != nil {
		t.Fatal(err)
	}
	b, err := baseline.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestExplainVessel は、器の違反で、許されている器の列挙をそのまま見せることを見る（設定ファイルを
// 開かずに、なぜ違反なのかが分かるところまで）。
func TestExplainVessel(t *testing.T) {
	res := &audit.Result{Findings: []audit.Finding{vessel1()}}

	var b strings.Builder
	if err := Explain(&b, cfg1(), "esorp.yaml", res, empty(t)); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `internal/store/index.go:8:2  place-not-allowed  place=leading kind=line
  // 前方移行はここで行っていた。
  この位置のコメントは許可されていません。
  severity: enforce — the default (esorp.yaml has no severity.place-not-allowed entry). It fails the run

  Decided by esorp.yaml at syntax.cstyle.allow:
    allow[0]  place: header
    allow[1]  place: doc  kind: [docline]  label: [TODO:, SAFETY:]  form: present
    place: leading (kind: line) is not in this enumeration. A comment in a vessel that was not enumerated is a violation, whatever its content

`)
}

// TestExplainForm は、書式の違反で、当たった指定を値ごと見せることを見る。
func TestExplainForm(t *testing.T) {
	f := vessel1()
	f.ID = rule.FormParagraphs
	f.Message = "段落は 1 つまでです。"
	f.Site = rule.Site{Path: "syntax.cstyle.allow[1].form.paragraphs", Allow: 1, Rule: -1}

	var b strings.Builder
	if err := Explain(&b, cfg1(), "esorp.yaml", &audit.Result{Findings: []audit.Finding{f}}, empty(t)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), `  Decided by esorp.yaml at syntax.cstyle.allow[1].form.paragraphs:
    paragraphs: 1
`) {
		t.Fatalf("書式の根拠が違います:\n%s", b.String())
	}
}

// TestExplainLabel は、札の違反で、その器で許されている札を並べることを見る。
func TestExplainLabel(t *testing.T) {
	f := vessel1()
	f.ID = rule.LabelRequired
	f.Message = "この器のコメントは札で始めてください。"
	f.Site = rule.Site{Path: "syntax.cstyle.allow[1].label", Allow: 1, Rule: -1}

	var b strings.Builder
	if err := Explain(&b, cfg1(), "esorp.yaml", &audit.Result{Findings: []audit.Finding{f}}, empty(t)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), `  Decided by esorp.yaml at syntax.cstyle.allow[1].label:
    label: [TODO:, SAFETY:]
    A comment in this vessel must begin with one of these
`) {
		t.Fatalf("札の根拠が違います:\n%s", b.String())
	}
}

// TestExplainLexicon は、層2 の違反で、当たったルールを見せること、継ぎ目に左右される当たりには
// check と同じ断りが text にも添うことを見る。
func TestExplainLexicon(t *testing.T) {
	res := &audit.Result{Findings: []audit.Finding{lexicon1()}}

	var b strings.Builder
	if err := Explain(&b, cfg1(), "esorp.yaml", res, empty(t)); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `internal/store/index.go:20:1  no-history  place=doc kind=docline
  // 以前はここで畳んでいた。
  履歴を書かないでください。
  This match depends on a line-wrap seam. The line wrapped at the boundary between half-width and full-width characters,
  and whether whitespace stood there in the original cannot be recovered. If there is nothing to fix in the original, put it on the baseline.
  severity: enforce — the default (esorp.yaml has no severity.no-history entry). It fails the run

  Decided by esorp.yaml at rules[0]:
    id: no-history
    pattern: 以前は|かつては
    where.syntax: [cstyle]
    where.kind: [docline]

`)
}

// TestExplainBaselined は、baseline が抑えている違反に、そう書き添えることを見る（explain には
// 出るが check には出ない、と言わなければ「なぜ check が黙るのか」が分からない）。
func TestExplainBaselined(t *testing.T) {
	f := vessel1()

	var b strings.Builder
	if err := Explain(&b, cfg1(), "esorp.yaml", &audit.Result{Findings: []audit.Finding{f}}, holds(t, f)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), "  This violation is held down by the baseline (it does not appear in check).\n") {
		t.Fatalf("baseline の断りが落ちています:\n%s", b.String())
	}
}

// TestExplainAdvisory は、advisory の違反で、強度の値だけでなく、それを決めた設定の場所まで出る
// ことを見る。explain は「その判断はどこから来たのか」を見せる面なので、強度も出どころを言う。
func TestExplainAdvisory(t *testing.T) {
	f := vessel1()
	f.Severity = config.SeverityAdvisory

	var b strings.Builder
	if err := Explain(&b, cfg1(), "esorp.yaml", &audit.Result{Findings: []audit.Finding{f}}, empty(t)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"place-not-allowed  [advisory]",
		"severity: advisory — decided by esorp.yaml at severity.place-not-allowed. It is reported, but it does not fail the run",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, b.String())
		}
	}
}

// TestExplainJSONAdvisory は、機械向けにも強度の出どころ（severity_path）が出ること、既定の enforce
// では出ないことを見る。決めた場所が設定のどこにも無いことを、欄の不在で告げる。
func TestExplainJSONAdvisory(t *testing.T) {
	f := vessel1()
	f.Severity = config.SeverityAdvisory
	res := &audit.Result{Comments: 1, Findings: []audit.Finding{f}}

	var b strings.Builder
	if err := ExplainJSON(&b, cfg1(), "esorp.yaml", "internal/store/index.go", 8, res, empty(t)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"severity": "advisory"`, `"severity_path": "severity.place-not-allowed"`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("出力に %q が現れない:\n%s", want, b.String())
		}
	}

	var enforced strings.Builder
	if err := ExplainJSON(&enforced, cfg1(), "esorp.yaml", "internal/store/index.go", 8, &audit.Result{Comments: 1, Findings: []audit.Finding{vessel1()}}, empty(t)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enforced.String(), "severity_path") {
		t.Errorf("既定の enforce なのに severity_path が出ています:\n%s", enforced.String())
	}
}

// TestExplainJSONVessel は、check の violations 1件に、根拠と baseline の状態を足した形を見る。
func TestExplainJSONVessel(t *testing.T) {
	res := &audit.Result{Comments: 1, Findings: []audit.Finding{vessel1()}}

	var b strings.Builder
	if err := ExplainJSON(&b, cfg1(), "esorp.yaml", "internal/store/index.go", 8, res, empty(t)); err != nil {
		t.Fatal(err)
	}

	wants(t, b.String(), `{
  "version": 1,
  "config": "esorp.yaml",
  "target": {
    "path": "internal/store/index.go",
    "line": 8
  },
  "status": "violated",
  "explanations": [
    {
      "path": "internal/store/index.go",
      "line": 8,
      "col": 2,
      "id": "place-not-allowed",
      "severity": "enforce",
      "place": "leading",
      "kind": "line",
      "text": "// 前方移行はここで行っていた。",
      "message": "この位置のコメントは許可されていません。",
      "baselined": false,
      "site": {
        "path": "syntax.cstyle.allow",
        "syntax": "cstyle",
        "allow": [
          {
            "place": "header"
          },
          {
            "place": "doc",
            "kind": [
              "docline"
            ],
            "label": [
              "TODO:",
              "SAFETY:"
            ],
            "form": {
              "subject": "declaration",
              "paragraphs": 1
            }
          }
        ]
      }
    }
  ]
}
`)
}

// TestExplainJSONLexicon は、層2 の違反で site.rule が立ち、site.allow が出ないことを見る。
func TestExplainJSONLexicon(t *testing.T) {
	res := &audit.Result{Comments: 1, Findings: []audit.Finding{lexicon1()}}

	var b strings.Builder
	if err := ExplainJSON(&b, cfg1(), "esorp.yaml", "internal/store/index.go", 20, res, empty(t)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), `      "seam_dependent": true,
      "baselined": false,
      "site": {
        "path": "rules[0]",
        "rule": {
          "id": "no-history",
          "pattern": "以前は|かつては",
          "message": "履歴を書かないでください。",
          "where": {
            "syntax": [
              "cstyle"
            ],
            "kind": [
              "docline"
            ]
          }
        }
      }`) {
		t.Fatalf("層2 の根拠が違います:\n%s", b.String())
	}
	if strings.Contains(b.String(), `"allow"`) {
		t.Error("層2 の違反なのに site.allow が出ています")
	}
}

// TestExplainJSONForm は、書式の違反で、当たった指定1つが site.form にキーと値で出ることを見る。
func TestExplainJSONForm(t *testing.T) {
	f := vessel1()
	f.ID = rule.FormParagraphs
	f.Site = rule.Site{Path: "syntax.cstyle.allow[1].form.paragraphs", Allow: 1, Rule: -1}

	var b strings.Builder
	if err := ExplainJSON(&b, cfg1(), "esorp.yaml", f.Path, f.Line, &audit.Result{Comments: 1, Findings: []audit.Finding{f}}, empty(t)); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), `        "form": {
          "key": "paragraphs",
          "value": 1
        }`) {
		t.Fatalf("書式の根拠が違います:\n%s", b.String())
	}
}

// TestExplainFormKeys は、違反 id と form のキーが一対一であることを見る（ずれれば、explain は
// 別の指定を根拠として見せる）。
func TestExplainFormKeys(t *testing.T) {
	paragraphs, maxLines := 2, 10
	cfg := &config.Config{Syntax: map[string]config.Syntax{"cstyle": {Allow: []config.Allow{{
		Place: "doc",
		Form: &config.Form{
			Subject:    "declaration",
			Headings:   "deny",
			Paragraphs: &paragraphs,
			MaxLines:   &maxLines,
			URLs:       "allow",
		},
	}}}}}

	for _, tc := range []struct {
		id   string
		text string
		key  string
		val  any
	}{
		{rule.FormSubject, "subject: declaration", "subject", "declaration"},
		{rule.FormHeadings, "headings: deny", "headings", "deny"},
		{rule.FormParagraphs, "paragraphs: 2", "paragraphs", float64(2)},
		{rule.FormMaxLines, "max_lines: 10", "max_lines", float64(10)},
		{rule.FormURLs, "urls: allow", "urls", "allow"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			f := vessel1()
			f.ID = tc.id
			f.Site = rule.Site{Path: "syntax.cstyle.allow[0].form." + tc.key, Allow: 0, Rule: -1}
			res := &audit.Result{Comments: 1, Findings: []audit.Finding{f}}

			var text, js strings.Builder
			if err := Explain(&text, cfg, "esorp.yaml", res, empty(t)); err != nil {
				t.Fatal(err)
			}
			if err := ExplainJSON(&js, cfg, "esorp.yaml", f.Path, f.Line, res, empty(t)); err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(text.String(), "    "+tc.text+"\n") {
				t.Errorf("text の根拠が %q ではありません:\n%s", tc.text, text.String())
			}

			var got struct {
				Explanations []struct {
					Site struct {
						Form *jsonFormKey `json:"form"`
					} `json:"site"`
				} `json:"explanations"`
			}
			decode(t, js.String(), &got)
			site := got.Explanations[0].Site
			if site.Form == nil || site.Form.Key != tc.key || site.Form.Value != tc.val {
				t.Errorf("JSON の根拠が違います: %+v（want key=%s value=%v）", site.Form, tc.key, tc.val)
			}
		})
	}
}

// TestExplainJSONStatus は、指した行が違反したのか・適合したのか・コメントが無かったのかを、
// status が書き分けることを見る（空の explanations では書き分けられない）。
func TestExplainJSONStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  *audit.Result
		want string
	}{
		{"違反あり", &audit.Result{Comments: 1, Findings: []audit.Finding{vessel1()}}, statusViolated},
		{"適合", &audit.Result{Comments: 1}, statusConforming},
		{"コメントが無い", &audit.Result{}, statusNoComment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			if err := ExplainJSON(&b, cfg1(), "esorp.yaml", "a.go", 1, tc.res, empty(t)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(b.String(), `"status": "`+tc.want+`"`) {
				t.Fatalf("status が %s ではありません:\n%s", tc.want, b.String())
			}
		})
	}
}

// TestCheckAndExplainShareViolationShape は、違反1件の形が check と explain で同じであることを見る
// （check の JSON で拾った違反を、そのまま explain に渡せる）。
func TestCheckAndExplainShareViolationShape(t *testing.T) {
	res := &audit.Result{Comments: 1, Findings: []audit.Finding{lexicon1()}}

	var check, explain strings.Builder
	if err := JSON(&check, res); err != nil {
		t.Fatal(err)
	}
	if err := ExplainJSON(&explain, cfg1(), "esorp.yaml", "internal/store/index.go", 20, res, empty(t)); err != nil {
		t.Fatal(err)
	}

	var c struct {
		Violations []jsonViolation `json:"violations"`
	}
	var e struct {
		Explanations []jsonExplanation `json:"explanations"`
	}
	decode(t, check.String(), &c)
	decode(t, explain.String(), &e)

	if len(c.Violations) != 1 || len(e.Explanations) != 1 {
		t.Fatalf("件数が合いません: check=%d explain=%d", len(c.Violations), len(e.Explanations))
	}
	if c.Violations[0] != e.Explanations[0].jsonViolation {
		t.Errorf("違反の形が食い違っています:\ncheck   = %+v\nexplain = %+v", c.Violations[0], e.Explanations[0].jsonViolation)
	}
}
