package config

import (
	"strings"
	"testing"
)

// diffOf は、body を手元の設定として読み、現行テンプレートとの差分を1つの文字列にして返す。
func diffOf(t *testing.T, body string) string {
	t.Helper()

	local, err := load(t, body)
	if err != nil {
		t.Fatalf("手元の設定が読めない: %v", err)
	}
	tmpl, err := TemplateConfig()
	if err != nil {
		t.Fatalf("テンプレートが読めない: %v", err)
	}

	var b strings.Builder
	for _, s := range Diff(local, tmpl) {
		b.WriteString(s.Title + "\n")
		for _, line := range s.Lines() {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// TestDiffTemplateAgainstItself は、テンプレートそのものを手元の設定として読ませたら差分が出ない
// ことを確かめる。ここが空にならないなら、差分の見方そのものが壊れている。
func TestDiffTemplateAgainstItself(t *testing.T) {
	if got := diffOf(t, Template); got != "" {
		t.Errorf("テンプレート自身との差分 = %q, want 空", got)
	}
}

// TestDiffPairsByFamilyAndFiles は、syntax エントリの対応づけが名前ではなくファミリと見るファイルで
// 行われることを確かめる。名前で照合すると、言語ごとに分けたテンプレート（cstyle-go / cstyle-rust）と
// 手元の1本（cstyle）が、丸ごと追加・削除として出てしまい、読めない。手元の設定は cstyle 1本で
// go と rs の両方を見ており、allow はテンプレートの cstyle-go とそっくり同じに書いてある——つまり
// Go の doc 規約（subject）を Rust にも課している。その食い違いが差分として見え、かつ差の無い
// cstyle-go の組は出ないことを見る。
func TestDiffPairsByFamilyAndFiles(t *testing.T) {
	got := diffOf(t, `
syntax:
  cstyle:
    files:
      - "**/*.go"
      - "!vendor/**"
      - "**/*.rs"
    mode: structural
    allow:
      - place: header
      - place: doc
        form:
          subject: required
          headings: deny
          paragraphs: 1
      - place: trailing
        label: ["SAFETY:", "TODO:", "nolint:"]
baseline: .esorp-baseline.json
`)

	if !strings.Contains(got, "paired with cstyle-rust in the template") {
		t.Errorf("cstyle が cstyle-rust に対応づいていない:\n%s", got)
	}
	if !strings.Contains(got, "allow[doc].form.subject  yours: required  template: (none)") {
		t.Errorf("subject の食い違いが出ていない:\n%s", got)
	}
	if strings.Contains(got, "syntax entries only in yours") {
		t.Errorf("対応づいたエントリが「手元だけ」として出ている:\n%s", got)
	}
	if strings.Contains(got, "paired with cstyle-go in the template") {
		t.Errorf("差の無い組が出ている:\n%s", got)
	}
}

// TestDiffFilesReportedOnce は、見にいくファイルの差が設定の全体で1回だけ出ることを確かめる。
// エントリごとに出すと、手元の1本が複数のテンプレートエントリを兼ねているとき、他のエントリが
// 拾っている glob まで「手元だけ」として何度も並ぶ。
func TestDiffFilesReportedOnce(t *testing.T) {
	got := diffOf(t, `
syntax:
  cstyle:
    files:
      - "**/*.go"
      - "**/*.rs"
    mode: structural
    allow:
      - place: header
baseline: .esorp-baseline.json
`)

	if n := strings.Count(got, "the files being looked at"); n != 1 {
		t.Errorf("「見にいくファイル」の節 = %d 個, want 1\n%s", n, got)
	}
	if strings.Contains(got, "yours only: **/*.go") || strings.Contains(got, "yours only: **/*.rs") {
		t.Errorf("テンプレートも見ている glob が「手元だけ」として出ている:\n%s", got)
	}
}

// TestDiffOnlyInTemplate は、テンプレートにあって手元に無いエントリが、取り込みの候補として挙がる
// ことを確かめる。使わない言語なら無いままでよいので、これは提案であって違反ではない。
func TestDiffOnlyInTemplate(t *testing.T) {
	got := diffOf(t, `
syntax:
  cstyle-go:
    family: cstyle
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
baseline: .esorp-baseline.json
`)

	if !strings.Contains(got, "syntax entries only in the template") {
		t.Errorf("テンプレートだけのエントリが挙がっていない:\n%s", got)
	}
	if !strings.Contains(got, "cssblock") {
		t.Errorf("cssblock が挙がっていない:\n%s", got)
	}
}

// TestDiffSeverity は、手元で弱めた強度が差分に出ること、そして値が見えることを確かめる。
// 強度は CI の赤/緑そのものなので、「違います」では取り込むかを決められない。
func TestDiffSeverity(t *testing.T) {
	got := diffOf(t, `
syntax:
  cstyle-go:
    family: cstyle
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
severity:
  form-paragraphs: advisory
`)

	if !strings.Contains(got, "severity") {
		t.Errorf("severity の節が出ていない:\n%s", got)
	}
	if !strings.Contains(got, "advisory") {
		t.Errorf("手元の強度の値が出ていない:\n%s", got)
	}
}

// TestDiffAllowAndRules は、器の有無・書式の値・層2 の語彙の差が出ることを確かめる。
func TestDiffAllowAndRules(t *testing.T) {
	got := diffOf(t, `
syntax:
  cstyle-go:
    family: cstyle
    files: ["**/*.go", "!vendor/**"]
    mode: structural
    allow:
      - place: header
      - place: doc
        form:
          subject: required
          headings: deny
          paragraphs: 2
      - place: leading
      - place: trailing
        label: ["SAFETY:", "TODO:", "nolint:"]
rules:
  - id: no-history
    pattern: "かつて"
    message: |
      変化を語っています。
  - id: no-jargon
    pattern: "とりま"
    message: |
      書き言葉で書いてください。
baseline: .esorp-baseline.json
`)

	for _, want := range []string{
		"allow[doc].form.paragraphs  yours: 2  template: 1",
		"allow[leading]  in yours only",
		"no-history.pattern  yours: かつて  template: ",
		"no-jargon  in yours only (your choice)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が出ていない:\n%s", want, got)
		}
	}
}
