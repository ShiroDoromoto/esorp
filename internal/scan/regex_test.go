package scan

import "testing"

// TestCStyleRegex は、正規表現リテラルを押さえる。中身は字句ではないので、そこに現れる引用符は
// 文字列を開かず、// はコメントにならない。逆に、除算の「/」を正規表現の開きと読めば、その行から
// 後ろのコメントを取り違える。分かれ目は直前のトークンだけ。
func TestCStyleRegex(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "正規表現の中の引用符は文字列を開かない（行末のコメントを飲み込まない）",
			src:  "const re = /don't/; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 21, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "正規表現の中の // はコメントではない",
			src: "const re = /https?:\\/\\/x/; // 実コメント\n" +
				"const n = 1;\n",
			want: []want{{line: 1, endLine: 1, col: 28, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "文字クラスの中の / は閉じない",
			src:  "const re = /[/'\"]/g; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 22, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "エスケープされた / は閉じない",
			src:  "const re = /a\\/'b/; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 21, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "除算は正規表現ではない（識別子・数値・閉じ括弧の後）",
			src: "const a = x / y;      // 実1\n" +
				"const b = 10 / 2 / 1; // 実2\n" +
				"const c = f(1) / 2;   // 実3\n" +
				"const d = xs[0] / 2;  // 実4\n",
			want: []want{
				{line: 1, endLine: 1, col: 23, kind: KindLine, text: "// 実1"},
				{line: 2, endLine: 2, col: 23, kind: KindLine, text: "// 実2"},
				{line: 3, endLine: 3, col: 23, kind: KindLine, text: "// 実3"},
				{line: 4, endLine: 4, col: 23, kind: KindLine, text: "// 実4"},
			},
		},
		{
			name: "引数・return・オブジェクトの値・配列の中でも正規表現として読む",
			src: "const a = s.replace(/'/g, \"\");  // 実1\n" +
				"function f() { return /'/.test(s); } // 実2\n" +
				"const o = { k: /'/ };  // 実3\n" +
				"const xs = [/'/, /\"/]; // 実4\n",
			want: []want{
				{line: 1, endLine: 1, col: 33, kind: KindLine, text: "// 実1"},
				{line: 2, endLine: 2, col: 38, kind: KindLine, text: "// 実2"},
				{line: 3, endLine: 3, col: 24, kind: KindLine, text: "// 実3"},
				{line: 4, endLine: 4, col: 24, kind: KindLine, text: "// 実4"},
			},
		},
		{
			name: "行内で閉じない「/」は正規表現ではない（後ろの字句を読み落とさない）",
			src: "const a = (1 + 2) / (3 + 4);\n" +
				"// 本物\n",
			want: []want{{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 本物"}},
		},
		{
			name: "コメントの // と /* は正規表現より先に読む",
			src: "// 行\n" +
				"/* ブロック */\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "// 行"},
				{line: 2, endLine: 2, col: 1, kind: KindBlock, text: "/* ブロック */"},
			},
		},
		{
			name: "文字列とテンプレートの中の / は正規表現ではない",
			src: "const u = \"a/b'c\";  // 実1\n" +
				"const v = `a/b'c`;  // 実2\n",
			want: []want{
				{line: 1, endLine: 1, col: 21, kind: KindLine, text: "// 実1"},
				{line: 2, endLine: 2, col: 21, kind: KindLine, text: "// 実2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comments(CStyle([]byte(tt.src), TSSpec()))
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

// TestCStyleRegexTSX は、JSX の { … } の中も同じく正規表現を読むことを押さえる（中はコード）。
func TestCStyleRegexTSX(t *testing.T) {
	src := "const a = <p>{s.replace(/'/g, \"\")}</p>; // 実コメント\n"
	got := comments(CStyle([]byte(src), TSXSpec()))
	if len(got) != 1 || got[0].Text != "// 実コメント" {
		t.Fatalf("コメント = %#v; want 1件の // 実コメント", got)
	}
}

// TestCStyleRegexOnlyWhereDeclared は、正規表現を持たない言語では「/」をリテラルの開きとして
// 読まないことを押さえる（Go の除算は除算）。
func TestCStyleRegexOnlyWhereDeclared(t *testing.T) {
	src := "const a = 1\nvar b = a / 2 // 実コメント\n"
	got := comments(CStyle([]byte(src), GoSpec()))
	if len(got) != 1 || got[0].Text != "// 実コメント" {
		t.Fatalf("コメント = %#v; want 1件の // 実コメント", got)
	}
}
