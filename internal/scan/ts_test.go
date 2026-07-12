package scan

import "testing"

// TestCStyleTS は、TS の字句を押さえる。Go / Rust との差はテンプレートリテラルで、
// ${ … } の中は再びコードなので、そこに現れるコメントはコメントとして読む。
func TestCStyleTS(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "JSDoc は docblock、// と /* */ はそのまま",
			src: "/** open は開く */\n" +
				"// ただの行\n" +
				"/* ただのブロック */\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindDocBlock, text: "/** open は開く */"},
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// ただの行"},
				{line: 3, endLine: 3, col: 1, kind: KindBlock, text: "/* ただのブロック */"},
			},
		},
		{
			name: "TS のブロックコメントはネストしない（最初の */ で閉じる）",
			src:  "/* 外 /* 内 */ const x = 1;\n",
			want: []want{{line: 1, endLine: 1, col: 1, kind: KindBlock, text: "/* 外 /* 内 */"}},
		},
		{
			name: "テンプレートリテラルの地の文にある // はコメントではない",
			src:  "const u = `http://example.com`; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 33, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "補間の中は再びコードなので、そこのコメントはコメント",
			src:  "const s = `a${ x /* 中 */ }b`; // 外\n",
			want: []want{
				{line: 1, endLine: 1, col: 18, kind: KindBlock, text: "/* 中 */"},
				{line: 1, endLine: 1, col: 33, kind: KindLine, text: "// 外"},
			},
		},
		{
			name: "補間の中の文字列に入った } ではテンプレートは閉じない",
			src:  "const s = `a${ f(\"}\") }b // 中`; // 外\n",
			want: []want{{line: 1, endLine: 1, col: 35, kind: KindLine, text: "// 外"}},
		},
		{
			name: "補間の中のオブジェクトリテラルの } でもテンプレートは閉じない",
			src:  "const s = `a${ g({ k: 1 }) }b // 中`; // 外\n",
			want: []want{{line: 1, endLine: 1, col: 40, kind: KindLine, text: "// 外"}},
		},
		{
			name: "テンプレートは入れ子になる",
			src:  "const s = `a${ `b${ c }d // 中` }e // 中`; // 外\n",
			want: []want{{line: 1, endLine: 1, col: 46, kind: KindLine, text: "// 外"}},
		},
		{
			name: "テンプレートは改行を含み、その中の // は無視され、行番号は進む",
			src: "const s = `\n" +
				"// テンプレートの中\n" +
				"`;\n" +
				"// 本物\n",
			want: []want{{line: 4, endLine: 4, col: 1, kind: KindLine, text: "// 本物"}},
		},
		{
			name: "補間の中の行コメントは、テンプレートの続きを飲み込まない",
			src: "const s = `a${ // 中\n" +
				"  x }b`;\n" +
				"// 本物\n",
			want: []want{
				{line: 1, endLine: 1, col: 16, kind: KindLine, text: "// 中"},
				{line: 3, endLine: 3, col: 1, kind: KindLine, text: "// 本物"},
			},
		},
		{
			name: "引用符の中の // はコメントではない",
			src:  "const u = 'http://example.com'; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 33, kind: KindLine, text: "// 実コメント"}},
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

// TestSpecForTS は、.ts と .tsx が別の字句を引くことを押さえる。.tsx に TS の字句を当てると、
// JSX テキストの中の // を行コメントと読む。
func TestSpecForTS(t *testing.T) {
	if spec, ok := SpecFor("src/app.ts"); !ok || spec.Name != "typescript" {
		t.Errorf("SpecFor(.ts) = %q, %v; want typescript, true", spec.Name, ok)
	}
	if spec, ok := SpecFor("src/app.tsx"); !ok || spec.Name != "tsx" || !spec.JSX {
		t.Errorf("SpecFor(.tsx) = %q, %v; want tsx, true", spec.Name, ok)
	}
	if _, ok := SpecFor("src/app.vue"); ok {
		t.Error("SpecFor(.vue) が字句を返した。持っていない字句は、持っていないと告げる")
	}
}
