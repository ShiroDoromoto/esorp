package rule

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

var formDisposition = map[string]string{
	FormSubject:    "宣言の名前で始めてください。",
	FormHeadings:   "doc コメントに見出しは書けません。",
	FormParagraphs: "doc コメントの段落は1つです。",
	FormRefs:       "追跡番号への参照です。",
	FormMaxLines:   "長すぎます。",
	FormURLs:       "URL は書けません。",
}

// templateForm は、テンプレートの既定と同じ書式（Go の doc）。
func templateForm() *config.Form {
	one := 1
	return &config.Form{Subject: "required", Headings: "deny", Paragraphs: &one, Refs: "deny"}
}

// form は、Go のソース断片の doc コメントを検査して、書式の違反 id を返す。
func form(t *testing.T, src string, f *config.Form) []string {
	t.Helper()

	return formWith(t, src, f, scan.GoSpec())
}

// formWith は、言語を指定して doc コメントを検査する。
func formWith(t *testing.T, src string, f *config.Form, spec scan.LangSpec) []string {
	t.Helper()

	allows := []config.Allow{{Place: "header"}, {Place: "doc", Form: f}, {Place: "trailing", Form: f}}

	target := Target{Syntax: "cstyle", Path: "a.go"}

	var out []string
	for _, c := range place.Classify(scan.CStyle([]byte(src), spec), spec) {
		i, v := Vessel(c, allows, formDisposition, target, spec)
		if v != nil {
			t.Fatalf("器で落ちている（書式のテストにならない）: %s %s %d", v.ID, v.Place, v.Line)
		}
		for _, fv := range Form(c, allows[i].Form, formDisposition, target, i, spec) {
			if fv.Message == "" {
				t.Errorf("%s: disposition のメッセージが引かれていない", fv.ID)
			}
			out = append(out, fv.ID+" "+strconv.Itoa(fv.Line))
		}
	}
	return out
}

func TestForm(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "宣言の名前で始まる1段落の doc は通る",
			src:  "package p\n\n// Open はストアを開く。\nfunc Open() error { return nil }\n",
		},
		{
			name: "宣言の名前で始まらない doc は form-subject",
			src:  "package p\n\n// 以前はここで前方移行していた。\nfunc Open() error { return nil }\n",
			want: []string{"form-subject 3"},
		},
		{
			name: "名前が別の識別子の一部なら、その宣言の説明ではない",
			src:  "package p\n\n// OpenFile を呼ぶ前に使う。\nfunc Open() error { return nil }\n",
			want: []string{"form-subject 3"},
		},
		{
			name: "見出しは form-headings（履歴は見出しを付けて書かれる）",
			src: "package p\n\n" +
				"// Open はストアを開く。\n" +
				"// # なぜ Open がもう移行しないのか\n" +
				"func Open() error { return nil }\n",
			want: []string{"form-headings 3"},
		},
		{
			name: "追跡番号 #123 は form-refs",
			src:  "package p\n\n// Open はストアを開く（#123 で変更）。\nfunc Open() error { return nil }\n",
			want: []string{"form-refs 3"},
		},
		{
			name: "追跡番号 ABC-123 も form-refs",
			src:  "package p\n\n// Open はストアを開く。ABC-123 を参照。\nfunc Open() error { return nil }\n",
			want: []string{"form-refs 3"},
		},
		{
			name: "付け足された背景の段落は form-paragraphs",
			src: "package p\n\n" +
				"// Open はストアを開く。\n" +
				"//\n" +
				"// もともとは前方移行もしていたが、今はしない。\n" +
				"func Open() error { return nil }\n",
			want: []string{"form-paragraphs 3"},
		},
		{
			name: "1つのコメントが複数の書式に反すれば、その全部を出す",
			src: "package p\n\n" +
				"// # 背景\n" +
				"//\n" +
				"// #42 で入った。\n" +
				"func Open() error { return nil }\n",
			want: []string{"form-subject 3", "form-headings 3", "form-paragraphs 3", "form-refs 3"},
		},
		{
			name: "宣言名を取り出せない doc は subject を検査しない（誤検知を出さない）",
			src:  "package p\n\n// 定数をまとめる。\nvar (\n\tx = 1\n)\n",
		},
		{
			name: "ブロックコメントでも記号を剥がして本文を見る",
			src:  "package p\n\n/* 以前はここで前方移行していた。 */\nfunc Open() error { return nil }\n",
			want: []string{"form-subject 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := form(t, tt.src, templateForm())
			if strings.Join(got, ", ") != strings.Join(tt.want, ", ") {
				t.Errorf("違反 = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFormParagraphsCountsProseOnly は、散文の段落だけが数えられることを確かめる。
func TestFormParagraphsCountsProseOnly(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "字下げされたコードブロックは段落と数えない（出力例を doc に置ける）",
			src: "package p\n\n" +
				"// Text は、人間向けの出力を書く。\n" +
				"//\n" +
				"//\ta.go:42:2  place-not-allowed\n" +
				"//\t  // 以前はこうだった\n" +
				"func Text() {}\n",
		},
		{
			name: "箇条書きは散文として数える（背景の逃げ場にしない）",
			src: "package p\n\n" +
				"// Text は、人間向けの出力を書く。\n" +
				"//\n" +
				"// - もともとは JSON だけだった\n" +
				"// - 今は text も出す\n" +
				"func Text() {}\n",
			want: []string{"form-paragraphs 3"},
		},
		{
			name: "空白での字下げはコードブロックと認めない（ずらすだけで抜けられてしまう）",
			src: "package p\n\n" +
				"// Text は、人間向けの出力を書く。\n" +
				"//\n" +
				"//   もともとは JSON だけだった。\n" +
				"func Text() {}\n",
			want: []string{"form-paragraphs 3"},
		},
		{
			name: "コードブロックのあとに散文の段落を足せば、それは数える",
			src: "package p\n\n" +
				"// Text は、人間向けの出力を書く。\n" +
				"//\n" +
				"//\ta.go:42:2  place-not-allowed\n" +
				"//\n" +
				"// もともとは JSON だけだった。\n" +
				"func Text() {}\n",
			want: []string{"form-paragraphs 3"},
		},
		{
			name: "コードブロックの中の # は見出しではない",
			src: "package p\n\n" +
				"// Text は、人間向けの出力を書く。\n" +
				"//\n" +
				"//\t# これはシェルのコメント\n" +
				"func Text() {}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := form(t, tt.src, templateForm())
			if strings.Join(got, ", ") != strings.Join(tt.want, ", ") {
				t.Errorf("違反 = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFormFencedCodeBlock は、doc が Markdown の言語（Rust / TypeScript）で、フェンスで囲んだ
// コードブロックを散文と数えないことを確かめる。Rust / TS の doc はコード例をフェンスで書くので、
// 数えると出力例を置いただけで誤爆する。Go の doc は Markdown ではないので、フェンスは器にならない。
func TestFormFencedCodeBlock(t *testing.T) {
	tests := []struct {
		name string
		src  string
		spec scan.LangSpec
		want []string
	}{
		{
			name: "Rust: フェンスの中は段落と数えない（コード例を doc に置ける）",
			src: "/// f は変換する。\n" +
				"///\n" +
				"/// ```\n" +
				"/// let x = f(1);\n" +
				"///\n" +
				"/// assert_eq!(x, 2);\n" +
				"/// ```\n" +
				"pub fn f() {}\n",
			spec: scan.RustSpec(),
		},
		{
			name: "Rust: フェンスの中の # は見出しではない",
			src: "/// f は変換する。\n" +
				"///\n" +
				"/// ```\n" +
				"/// # let x = 1;\n" +
				"/// ```\n" +
				"pub fn f() {}\n",
			spec: scan.RustSpec(),
		},
		{
			name: "Rust: フェンスを閉じ直せば、そのあとの散文は数える（囲んで隠せない）",
			src: "/// f は変換する。\n" +
				"///\n" +
				"/// ```\n" +
				"/// let x = f(1);\n" +
				"/// ```\n" +
				"///\n" +
				"/// もともとは同期だった。\n" +
				"pub fn f() {}\n",
			spec: scan.RustSpec(),
			want: []string{"form-paragraphs 1"},
		},
		{
			name: "TypeScript: JSDoc のフェンスの中は段落と数えない",
			src: "/**\n" +
				" * f は変換する。\n" +
				" *\n" +
				" * ```ts\n" +
				" * const x = f(1);\n" +
				" *\n" +
				" * console.log(x);\n" +
				" * ```\n" +
				" */\n" +
				"export function f() {}\n",
			spec: scan.TSSpec(),
		},
		{
			name: "Go: フェンスは器と認めない（doc は Markdown ではなく、認めれば背景の逃げ場になる）",
			src: "package p\n\n" +
				"// Text は、人間向けの出力を書く。\n" +
				"//\n" +
				"// ```\n" +
				"// もともとは JSON だけだった。\n" +
				"// ```\n" +
				"func Text() {}\n",
			spec: scan.GoSpec(),
			want: []string{"form-paragraphs 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formWith(t, tt.src, templateForm(), tt.spec)
			if strings.Join(got, ", ") != strings.Join(tt.want, ", ") {
				t.Errorf("違反 = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFormOmitted は、form: を書かなければ書式を問わないことを確かめる。省略したものは検査しない。
func TestFormOmitted(t *testing.T) {
	src := "package p\n\n// # 見出しも #42 も段落も、\n//\n// form が無ければ問われない。\nfunc Open() {}\n"
	if got := form(t, src, nil); len(got) != 0 {
		t.Errorf("form: が無いのに検査している: %v", got)
	}
	if got := form(t, src, &config.Form{}); len(got) != 0 {
		t.Errorf("空の form: で検査している: %v", got)
	}
}

// TestFormMaxLinesAndURLs は、max_lines / urls が書いたときだけ効くことを確かめる（既定は off）。
func TestFormMaxLinesAndURLs(t *testing.T) {
	src := "package p\n\n" +
		"// Open はストアを開く。\n" +
		"// 詳細は https://example.com/spec を見よ。\n" +
		"func Open() {}\n"

	if got := form(t, src, &config.Form{}); len(got) != 0 {
		t.Fatalf("max_lines / urls は既定で効かないはず: %v", got)
	}

	one := 1
	got := form(t, src, &config.Form{MaxLines: &one, URLs: "deny"})
	want := []string{"form-max-lines 3", "form-urls 3"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("違反 = %v, want %v", got, want)
	}
}
