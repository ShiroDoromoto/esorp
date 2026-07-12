package place

import (
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/scan"
)

type want struct {
	line    int
	endLine int
	place   Place
	decl    string
	text    string
}

func check(t *testing.T, src string, spec scan.LangSpec, wants []want) {
	t.Helper()

	got := Classify(scan.CStyle([]byte(src), spec), spec)
	if len(got) != len(wants) {
		t.Fatalf("器の数 = %d, want %d\n得たもの: %#v", len(got), len(wants), got)
	}
	for i, w := range wants {
		g := got[i]
		if g.Place != w.place || g.Line != w.line || g.EndLine != w.endLine || g.Decl != w.decl || g.Text != w.text {
			t.Errorf("comment[%d] = {%v %d-%d decl=%q %q}, want {%v %d-%d decl=%q %q}",
				i, g.Place, g.Line, g.EndLine, g.Decl, g.Text,
				w.place, w.line, w.endLine, w.decl, w.text)
		}
	}
}

func TestClassifyGo(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "ファイル冒頭は header（前にコードトークンが1つも無い）",
			src: "// Package p は何かをする。\n" +
				"package p\n",
			want: []want{{line: 1, endLine: 1, place: Header, text: "// Package p は何かをする。"}},
		},
		{
			name: "トップレベルの宣言の直前は doc。宣言名を取り出す",
			src: "package p\n" +
				"\n" +
				"// Open はストアを開く。\n" +
				"func Open(path string) error { return nil }\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "Open", text: "// Open はストアを開く。"}},
		},
		{
			name: "メソッドの doc はレシーバを飛ばしてメソッド名を取り出す",
			src: "package p\n" +
				"\n" +
				"// Scan は字句に分解する。\n" +
				"func (s *Scanner) Scan(src []byte) []Token { return nil }\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "Scan", text: "// Scan は字句に分解する。"}},
		},
		{
			name: "型の中のフィールドの直前も doc（type-like スコープ）",
			src: "package p\n" +
				"\n" +
				"type Token struct {\n" +
				"\t// Line は 1 始まり。\n" +
				"\tLine int\n" +
				"}\n",
			want: []want{{line: 4, endLine: 4, place: Doc, decl: "Line", text: "// Line は 1 始まり。"}},
		},
		{
			name: "関数本体の中は、宣言の直前でも doc にならず leading",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\t// 以前はここで前方移行していた。\n" +
				"\tvar x int\n" +
				"\t_ = x\n" +
				"}\n",
			want: []want{{line: 4, endLine: 4, place: Leading, text: "// 以前はここで前方移行していた。"}},
		},
		{
			name: "関数本体の中の文の直前は leading",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\t// ここで初期化する。\n" +
				"\tg()\n" +
				"}\n",
			want: []want{{line: 4, endLine: 4, place: Leading, text: "// ここで初期化する。"}},
		},
		{
			name: "行末にぶら下がるのは trailing",
			src: "package p\n" +
				"\n" +
				"var x = 1 // SAFETY: 何か\n",
			want: []want{{line: 3, endLine: 3, place: Trailing, text: "// SAFETY: 何か"}},
		},
		{
			name: "閉じ括弧の直前は orphan",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\tg()\n" +
				"\t// もう呼ばない。\n" +
				"}\n",
			want: []want{{line: 5, endLine: 5, place: Orphan, text: "// もう呼ばない。"}},
		},
		{
			name: "次のコードとの間に空行があれば orphan",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\t// 事情のメモ。\n" +
				"\n" +
				"\tg()\n" +
				"}\n",
			want: []want{{line: 4, endLine: 4, place: Orphan, text: "// 事情のメモ。"}},
		},
		{
			name: "ファイル末尾は orphan",
			src: "package p\n" +
				"\n" +
				"var x = 1\n" +
				"\n" +
				"// 置き去りのメモ。\n",
			want: []want{{line: 5, endLine: 5, place: Orphan, text: "// 置き去りのメモ。"}},
		},
		{
			name: "連続する複数行コメントは1つの器。先頭で判定し、塊全体に適用する",
			src: "package p\n" +
				"\n" +
				"// Open はストアを開く。\n" +
				"// 2行目。\n" +
				"// 3行目。\n" +
				"func Open() {}\n",
			want: []want{{
				line: 3, endLine: 5, place: Doc, decl: "Open",
				text: "// Open はストアを開く。\n// 2行目。\n// 3行目。",
			}},
		},
		{
			name: "空行で切れたコメントは別の器",
			src: "package p\n" +
				"\n" +
				"// 離れたメモ。\n" +
				"\n" +
				"// Open はストアを開く。\n" +
				"func Open() {}\n",
			want: []want{
				{line: 3, endLine: 3, place: Orphan, text: "// 離れたメモ。"},
				{line: 5, endLine: 5, place: Doc, decl: "Open", text: "// Open はストアを開く。"},
			},
		},
		{
			name: "行末コメントは、次の行のコメントと塊にならない",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\tg() // 行末。\n" +
				"\t// 次の行。\n" +
				"\th()\n" +
				"}\n",
			want: []want{
				{line: 4, endLine: 4, place: Trailing, text: "// 行末。"},
				{line: 5, endLine: 5, place: Leading, text: "// 次の行。"},
			},
		},
		{
			name: "括弧でまとめた宣言は doc だが紐づく名前が無い",
			src: "package p\n" +
				"\n" +
				"// 終了コードの規約。\n" +
				"const (\n" +
				"\tok = 0\n" +
				")\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "", text: "// 終了コードの規約。"}},
		},
		{
			name: "関数リテラルの中も func スコープなので doc にならない",
			src: "package p\n" +
				"\n" +
				"var f = func() {\n" +
				"\t// メモ。\n" +
				"\tvar x int\n" +
				"\t_ = x\n" +
				"}\n",
			want: []want{{line: 4, endLine: 4, place: Leading, text: "// メモ。"}},
		},
		{
			name: "型の中の関数本体（メソッド）を抜ければ、また doc になる",
			src: "package p\n" +
				"\n" +
				"func (s *S) A() {\n" +
				"\t// 中のメモ。\n" +
				"\tg()\n" +
				"}\n" +
				"\n" +
				"// B は何かをする。\n" +
				"func (s *S) B() {}\n",
			want: []want{
				{line: 4, endLine: 4, place: Leading, text: "// 中のメモ。"},
				{line: 8, endLine: 8, place: Doc, decl: "B", text: "// B は何かをする。"},
			},
		},
		{
			name: "ブロックコメントの終端行から数えて空行を見る",
			src: "package p\n" +
				"\n" +
				"/*\n" +
				"複数行の doc。\n" +
				"*/\n" +
				"func Open() {}\n",
			want: []want{{line: 3, endLine: 5, place: Doc, decl: "Open", text: "/*\n複数行の doc。\n*/"}},
		},
		{
			name: "文字列リテラル中の // は器を作らない",
			src: "package p\n" +
				"\n" +
				"var u = \"http://example.com\"\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.src, scan.GoSpec(), tt.want)
		})
	}
}

// doc 専用記法を持つ言語では、字句だけで doc と分かる。位置判定より kind を優先する。
func TestClassifyDocNotationWins(t *testing.T) {
	spec := scan.LangSpec{
		Name:            "doc-notation",
		LineComment:     "//",
		BlockOpen:       "/*",
		BlockClose:      "*/",
		DocLine:         []string{"///", "//!"},
		DocBlock:        []string{"/**"},
		DeclKeywords:    []string{"fn", "struct", "mod"},
		TypeLikeOpeners: []string{"struct", "impl"},
		FuncOpeners:     []string{"fn"},
	}
	src := "//! ファイル冒頭でも header ではなく doc\n" +
		"\n" +
		"/// open は開く\n" +
		"fn open() {\n" +
		"\t/// 関数の中でも記法が doc なら doc\n" +
		"\tlet x = 1;\n" +
		"}\n"

	// 冒頭の //! に続く宣言の名前が付いてしまうのは、内側 doc（それを囲むものを説明する記法）を
	// まだ知らないため。Rust を載せるとき（#11）に LangSpec で切り分ける。subject は Rust では
	// 既定 off なので、今のところ実害は無い。
	check(t, src, spec, []want{
		{line: 1, endLine: 1, place: Doc, decl: "open", text: "//! ファイル冒頭でも header ではなく doc"},
		{line: 3, endLine: 3, place: Doc, decl: "open", text: "/// open は開く"},
		{line: 5, endLine: 5, place: Doc, decl: "", text: "/// 関数の中でも記法が doc なら doc"},
	})
}
