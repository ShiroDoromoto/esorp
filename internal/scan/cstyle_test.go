package scan

import (
	"strings"
	"testing"
)

// want は、期待するコメントトークン1つ。
type want struct {
	line    int
	endLine int
	col     int
	kind    Kind
	text    string
}

// comments は、コメントトークンだけを取り出す。
// 期待値をコメントに絞れるのは、罠がすべて「コメントでないものをコメントと読む」側に出るため。
func comments(toks []Token) []Token {
	var out []Token
	for _, t := range toks {
		if t.Kind.IsComment() {
			out = append(out, t)
		}
	}
	return out
}

func TestCStyleGo(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "行コメントとブロックコメント",
			src: "package p\n" +
				"// 行コメント\n" +
				"/* ブロック */\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 行コメント"},
				{line: 3, endLine: 3, col: 1, kind: KindBlock, text: "/* ブロック */"},
			},
		},
		{
			name: "文字列リテラル中の // はコメントではない",
			src:  "u := \"http://example.com\" // 実コメント\n",
			want: []want{
				{line: 1, endLine: 1, col: 27, kind: KindLine, text: "// 実コメント"},
			},
		},
		{
			name: "文字列リテラル中の /* もコメントではない",
			src:  "s := \"/* これは文字列 */\"\n",
			want: nil,
		},
		{
			name: "生文字列は改行を含み、その中の // は無視され、行番号は進む",
			src: "s := `\n" +
				"// 生文字列の中\n" +
				"/* ここも */\n" +
				"`\n" +
				"// 本物\n",
			want: []want{
				{line: 5, endLine: 5, col: 1, kind: KindLine, text: "// 本物"},
			},
		},
		{
			name: "エスケープされた引用符で文字列は閉じない",
			src:  "s := \"\\\" // 中\" // 外\n",
			want: []want{
				{line: 1, endLine: 1, col: 18, kind: KindLine, text: "// 外"},
			},
		},
		{
			name: "ルーンリテラル中の引用符",
			src:  "c := '\\'' // 後ろ\n",
			want: []want{
				{line: 1, endLine: 1, col: 11, kind: KindLine, text: "// 後ろ"},
			},
		},
		{
			name: "ブロックコメントは複数行にまたがり EndLine を持つ",
			src: "/*\n" +
				"複数行\n" +
				"*/\n" +
				"package p\n",
			want: []want{
				{line: 1, endLine: 3, col: 1, kind: KindBlock, text: "/*\n複数行\n*/"},
			},
		},
		{
			name: "ブロックコメント中の引用符は文字列を開かない",
			src: "/* \" ここは文字列ではない */\n" +
				"// 次\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindBlock, text: "/* \" ここは文字列ではない */"},
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 次"},
			},
		},
		{
			name: "Go のブロックコメントはネストしない（最初の */ で閉じる）",
			src:  "/* 外 /* 内 */ x := 1\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindBlock, text: "/* 外 /* 内 */"},
			},
		},
		{
			name: "Go には doc 記法が無いので /// も //! も行コメント",
			src: "/// doc ではない\n" +
				"//! これも\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "/// doc ではない"},
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "//! これも"},
			},
		},
		{
			name: "/**/ は空のブロックコメント",
			src:  "/**/\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindBlock, text: "/**/"},
			},
		},
		{
			name: "行末コメントの列はバイト数で数える（多バイト文字の後ろ）",
			src:  "s := \"あ\" // 末尾\n",
			want: []want{
				{line: 1, endLine: 1, col: 12, kind: KindLine, text: "// 末尾"},
			},
		},
		{
			name: "CRLF でも行コメントに \\r を含めない",
			src:  "// crlf\r\npackage p\r\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "// crlf"},
			},
		},
		{
			name: "閉じていないブロックコメントは EOF まで",
			src:  "/* 閉じない\nつづき\n",
			want: []want{
				{line: 1, endLine: 3, col: 1, kind: KindBlock, text: "/* 閉じない\nつづき\n"},
			},
		},
		{
			name: "コメントの無いソース",
			src:  "package p\n\nfunc f() {}\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comments(CStyle([]byte(tt.src), GoSpec()))
			if len(got) != len(tt.want) {
				t.Fatalf("コメント数 = %d, want %d\n得たもの: %#v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.Kind != w.kind || g.Line != w.line || g.EndLine != w.endLine || g.Col != w.col || g.Text != w.text {
					t.Errorf("comment[%d] = {%v %d-%d:%d %q}, want {%v %d-%d:%d %q}",
						i, g.Kind, g.Line, g.EndLine, g.Col, g.Text,
						w.kind, w.line, w.endLine, w.col, w.text)
				}
			}
		})
	}
}

// TestCStyleKeepsCodeTokens は、コメント以外のトークンを落とさずに返していることを押さえる。
// スコープと宣言の判定（internal/place）が、それらを見て行われるため。
func TestCStyleKeepsCodeTokens(t *testing.T) {
	src := "func f() {\n\t// 中\n}\n"

	var got []string
	for _, tok := range CStyle([]byte(src), GoSpec()) {
		got = append(got, tok.Kind.String()+":"+tok.Text)
	}

	want := "word:func word:f punct:( punct:) punct:{ line:// 中 punct:}"
	if strings.Join(got, " ") != want {
		t.Errorf("tokens = %q, want %q", strings.Join(got, " "), want)
	}
}
