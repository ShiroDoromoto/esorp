package scan

import (
	"slices"
	"testing"
)

func TestBody(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"行コメントの記号と空白を剥がす", "//   SAFETY: 呼び出し側が保証する", "SAFETY: 呼び出し側が保証する"},
		{"塊は行ごとに剥がす", "// 1行目。\n// 2行目。", "1行目。\n2行目。"},
		{"ブロックコメントの記号を剥がす", "/* ラベル */", "ラベル"},
		{"複数行ブロックの継ぎ行の * と、記号だけの行を落とす", "/*\n * 1行目。\n * 2行目。\n */", "1行目。\n2行目。"},
		{"中の空行は段落の区切りなので残す", "// 1段落目。\n//\n// 2段落目。", "1段落目。\n\n2段落目。"},
		{"本文そのものには手を触れない", "// # 見出し", "# 見出し"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Body(tt.text, GoSpec()); got != tt.want {
				t.Errorf("Body(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestBodyDocNotation(t *testing.T) {
	spec := LangSpec{LineComment: "//", BlockOpen: "/*", BlockClose: "*/", DocLine: []string{"///", "//!"}, DocBlock: []string{"/**"}}
	if got := Body("/// open は開く", spec); got != "open は開く" {
		t.Errorf("docline = %q", got)
	}
	if got := Body("/** JSDoc */", spec); got != "JSDoc" {
		t.Errorf("docblock = %q", got)
	}
}

// TestBodyLinesKeepsInnerIndent は、コメント記号の内側の字下げが残ることを見る。外側とするのは継ぎ行
// に共通する字下げだけで、それを超えるタブは、開きが行のどこにあっても内側として残る。
func TestBodyLinesKeepsInnerIndent(t *testing.T) {
	ts := LangSpec{Name: "ts", LineComment: "//", BlockOpen: "/*", BlockClose: "*/", BlockStars: true, DocBlock: []string{"/**"}, DocFences: true}

	tests := []struct {
		name string
		text string
		spec LangSpec
		want []string
	}{
		{
			name: "Go のブロック doc のコードブロック",
			text: "/*\nText は書く。\n\n\ta.go:42:2  place-not-allowed\n*/",
			spec: GoSpec(),
			want: []string{"Text は書く。", "", "\ta.go:42:2  place-not-allowed"},
		},
		{
			name: "字下げした場所のブロックコメントは、共通する字下げだけを剥がす",
			text: "/*\n\t説明。\n\n\t\tcode()\n\t*/",
			spec: GoSpec(),
			want: []string{"説明。", "", "\tcode()"},
		},
		{
			name: "行の途中で開いても、継ぎ行のタブは内側として残る",
			text: "/*\nF は変換する。\n\n\tcode()\n*/",
			spec: GoSpec(),
			want: []string{"F は変換する。", "", "\tcode()"},
		},
		{
			name: "全部の行が字下げされていれば、それは外側であって、コードブロックではない",
			text: "/*\n\tText は書く。\n\n\tもともとは JSON だけだった。\n*/",
			spec: GoSpec(),
			want: []string{"Text は書く。", "", "もともとは JSON だけだった。"},
		},
		{
			name: "JSDoc の継ぎ行の * は空白ごと落とす",
			text: "/**\n * Text は書く。\n *\n * ```\n * code()\n * ```\n */",
			spec: ts,
			want: []string{"Text は書く。", "", "```", "code()", "```"},
		},
		{
			name: "コードブロックの中の * 始まりの行を継ぎ記号と読み違えない",
			text: "/*\nF は書き込む。\n\n\t*p = 1\n*/",
			spec: GoSpec(),
			want: []string{"F は書き込む。", "", "\t*p = 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BodyLines(tt.text, tt.spec)
			if !slices.Equal(got, tt.want) {
				t.Errorf("BodyLines(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestUnwrap(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"英語の折り返しは空白でつなぐ", []string{"G does not do this no", "longer."}, "G does not do this no longer."},
		{"日本語の折り返しは空白を挟まない", []string{"F はかつ", "て同期だった。"}, "F はかつて同期だった。"},
		{"全角と半角の境目には空白を挟む", []string{"本文は", "Body が作る。"}, "本文は Body が作る。"},
		{"長音で折り返しても空白を挟まない", []string{"サーバ", "ー。"}, "サーバー。"},
		{"約物で始まる行でも空白を挟まない", []string{"これは", "「器」だ。"}, "これは「器」だ。"},
		{"段落の区切りは残す", []string{"1段落目。", "", "2段落目。"}, "1段落目。\n2段落目。"},
		{"畳む先が無ければそのまま", []string{"1行だけ。"}, "1行だけ。"},
		{"空の本文は空のまま", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Unwrap(tt.lines, GoSpec()).Text; got != tt.want {
				t.Errorf("Unwrap(%q) = %q, want %q", tt.lines, got, tt.want)
			}
		})
	}
}

// TestUnwrapKeepsParagraphsApart は、畳んだ段落どうしが地続きにならないことを見る。地続きにすると、
// 段落をまたいだ句に当たってしまう。
func TestUnwrapKeepsParagraphsApart(t *testing.T) {
	if got := Unwrap(BodyLines("// no\n//\n// longer", GoSpec()), GoSpec()).Text; got != "no\nlonger" {
		t.Errorf("Unwrap = %q, want %q", got, "no\nlonger")
	}
}

// TestUnwrapKeepsCodeBlockLines は、doc のコードブロックの行を畳まないことを見る。畳むとコードの行
// どうしが空白でつながり、層2 の正規表現が行をまたいで当たりうる。前後の散文とも地続きにしない。
func TestUnwrapKeepsCodeBlockLines(t *testing.T) {
	text := "// F は変換する。\n//\n//\tif x == nil {\n//\t\treturn\n//\t}\n//\n// 呼ぶ側は\n// 気にしない。"
	want := "F は変換する。\n\tif x == nil {\n\t\treturn\n\t}\n呼ぶ側は気にしない。"
	if got := Unwrap(BodyLines(text, GoSpec()), GoSpec()).Text; got != want {
		t.Errorf("Unwrap = %q, want %q", got, want)
	}
}

// TestUnwrapMarksUncertainSeams は、原文に空白が在ったかを復元できない継ぎ目——半角と全角の境目——
// だけに印が付くことを見る。同じ幅どうしの継ぎ目は、空白の有無が折り返しから決まるので印を持たない。
func TestUnwrapMarksUncertainSeams(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{"半角と全角の境目は不確か", []string{"互換の境界は v2", "以前は無視する。"}, 1},
		{"全角と半角の境目も不確か", []string{"本文は", "Body が作る。"}, 1},
		{"全角どうしは確か", []string{"F はかつ", "て同期だった。"}, 0},
		{"半角どうしは確か", []string{"no", "longer."}, 0},
		{"段落をまたいでも継ぎ目にはならない", []string{"境界は v2", "", "以前は無視する。"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(Unwrap(tt.lines, GoSpec()).Uncertain); got != tt.want {
				t.Errorf("Unwrap(%q).Uncertain = %d 件, want %d 件", tt.lines, got, tt.want)
			}
		})
	}
}

// TestFoldedReadings は、不確かな継ぎ目が2つの読みを生むことを見る。継ぎ目に原文の空白が在ったかは
// 折り返した時点で失われているので、どちらの読みが原文かは決められない。片方に賭ければ、賭けを外した
// 側で黙って誤爆するか、黙って取りこぼす。
func TestFoldedReadings(t *testing.T) {
	got := Unwrap([]string{"互換の境界は v2", "以前は無視する。"}, GoSpec()).Readings()
	want := []string{"互換の境界は v2 以前は無視する。", "互換の境界は v2以前は無視する。"}
	if !slices.Equal(got, want) {
		t.Errorf("Readings = %q, want %q", got, want)
	}

	if got := Unwrap([]string{"no", "longer."}, GoSpec()).Readings(); !slices.Equal(got, []string{"no longer."}) {
		t.Errorf("確かな継ぎ目だけなら読みは1つ: Readings = %q", got)
	}
}

// TestCodeLines は、コードブロックの行の見分けを確かめる。行ごとに独立して見分けられるのはタブの
// 字下げだけで、フェンスは開きと閉じの間という状態を持つ。
func TestCodeLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		spec  LangSpec
		want  []bool
	}{
		{
			"タブの字下げはコードブロック",
			[]string{"f は変換する。", "", "\tlet x = 1;"},
			GoSpec(),
			[]bool{false, false, true},
		},
		{
			"フェンスは、その行も含めてコードブロック",
			[]string{"f は変換する。", "```", "let x = 1;", "```", "呼ぶ側は気にしない。"},
			RustSpec(),
			[]bool{false, true, true, true, false},
		},
		{
			"フェンスに言語を添えても開きと読む",
			[]string{"```ts", "const x = 1;", "```"},
			TSSpec(),
			[]bool{true, true, true},
		},
		{
			"閉じないフェンスは、本文の終わりまでコードブロック",
			[]string{"f は変換する。", "```", "let x = 1;"},
			RustSpec(),
			[]bool{false, true, true},
		},
		{
			"doc が Markdown でない言語（Go）では、フェンスは器にならない",
			[]string{"Text は出力を書く。", "```", "もともとは JSON だけだった。", "```"},
			GoSpec(),
			[]bool{false, false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CodeLines(tt.lines, tt.spec)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CodeLines(%q) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}
