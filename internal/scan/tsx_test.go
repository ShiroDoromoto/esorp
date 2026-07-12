package scan

import "testing"

// TestCStyleTSX は、TSX の字句を押さえる。要は「JSX テキストの中に字句は無い」ことと、
// それでも「{ … } の中は再びコード」であること。前者を落とすと誤検知し、後者を落とすと
// JSX のコメント（{/* … */}）を見落とす。
func TestCStyleTSX(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "JSX テキストの中の // はコメントではない",
			src: "const a = <p>詳しくは http://example.com</p>; // 実コメント\n" +
				"const b = 1;\n",
			want: []want{{line: 1, endLine: 1, col: 51, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "JSX テキストの中の ' は文字列を開かない（行末のコメントを飲み込まない）",
			src:  "const a = <p>it's fine</p>; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 29, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "JSX のコメントは {/* … */}",
			src: "const a = (\n" +
				"  <div>\n" +
				"    {/* 中 */}\n" +
				"  </div>\n" +
				");\n",
			want: []want{{line: 3, endLine: 3, col: 6, kind: KindBlock, text: "/* 中 */"}},
		},
		{
			name: "テキストの中の不等号はタグを開かない",
			src:  "const a = <p>1 < 2</p>; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 25, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "属性の値は文字列とコード（そこのコメントはコメント）",
			src:  "const a = <A b=\"//x\" c={/* 中 */ y} />; // 外\n",
			want: []want{
				{line: 1, endLine: 1, col: 25, kind: KindBlock, text: "/* 中 */"},
				{line: 1, endLine: 1, col: 42, kind: KindLine, text: "// 外"},
			},
		},
		{
			name: "入れ子・フラグメント・自己閉じを抜けて、後ろのコメントに戻る",
			src: "const a = (\n" +
				"  <>\n" +
				"    <A>\n" +
				"      <B x={1} />\n" +
				"      テキスト // ここはコメントではない\n" +
				"    </A>\n" +
				"  </>\n" +
				");\n" +
				"// 本物\n",
			want: []want{{line: 9, endLine: 9, col: 1, kind: KindLine, text: "// 本物"}},
		},
		{
			name: "子の式の中の JSX も読む",
			src: "const a = <ul>{xs.map((x) => <li>{x /* 中 */}</li>)}</ul>;\n" +
				"// 本物\n",
			want: []want{
				{line: 1, endLine: 1, col: 37, kind: KindBlock, text: "/* 中 */"},
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 本物"},
			},
		},
		{
			name: "ジェネリクスの < は要素を開かない",
			src: "const [s, set] = useState<string>('a'); // 実コメント\n" +
				"function f<T>(x: T): T { return x; } // 実コメント2\n",
			want: []want{
				{line: 1, endLine: 1, col: 41, kind: KindLine, text: "// 実コメント"},
				{line: 2, endLine: 2, col: 38, kind: KindLine, text: "// 実コメント2"},
			},
		},
		{
			name: "ジェネリクスのアロー関数（要素と紛れる）を飲み込まない",
			src: "const id = <T extends unknown>(x: T): T => x;\n" +
				"// 本物\n" +
				"const a = <div>hi</div>;\n" +
				"// 本物2\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 本物"},
				{line: 4, endLine: 4, col: 1, kind: KindLine, text: "// 本物2"},
			},
		},
		{
			name: "閉じない要素でも、後ろの字句を読み落とさない",
			src: "const a = <div>\n" +
				"// 本物\n",
			want: []want{{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 本物"}},
		},
		{
			name: "return の後の要素も読む",
			src: "function f() {\n" +
				"  return <p>a // b</p>;\n" +
				"}\n" +
				"// 本物\n",
			want: []want{{line: 4, endLine: 4, col: 1, kind: KindLine, text: "// 本物"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comments(CStyle([]byte(tt.src), TSXSpec()))
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
