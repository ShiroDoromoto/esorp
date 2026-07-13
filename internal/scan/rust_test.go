package scan

import "testing"

// TestScanRust は、Go との差（ネスト・可変長の生文字列・doc 記法・ライフタイム）がすべて
// LangSpec に収まっていることの検証を兼ねる（スキャナ本体は言語を知らない）。
func TestScanRust(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "doc 記法は字句で分かる（/// //! /** /*!）",
			src: "//! モジュールの doc\n" +
				"/// open は開く\n" +
				"/** doc ブロック */\n" +
				"/*! 内側の doc ブロック */\n" +
				"// ただの行\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindDocLine, text: "//! モジュールの doc"},
				{line: 2, endLine: 2, col: 1, kind: KindDocLine, text: "/// open は開く"},
				{line: 3, endLine: 3, col: 1, kind: KindDocBlock, text: "/** doc ブロック */"},
				{line: 4, endLine: 4, col: 1, kind: KindDocBlock, text: "/*! 内側の doc ブロック */"},
				{line: 5, endLine: 5, col: 1, kind: KindLine, text: "// ただの行"},
			},
		},
		{
			name: "記号を重ねた区切り線は doc ではない",
			src:  "//////////\n",
			want: []want{{line: 1, endLine: 1, col: 1, kind: KindLine, text: "//////////"}},
		},
		{
			name: "ブロックコメントはネストする（内側の */ では閉じない）",
			src: "/* 外 /* 内 */ まだ中 */\n" +
				"let x = 1;\n",
			want: []want{{line: 1, endLine: 1, col: 1, kind: KindBlock, text: "/* 外 /* 内 */ まだ中 */"}},
		},
		{
			name: "生文字列 r\"…\" の中の // はコメントではない",
			src:  "let s = r\"http://example.com\"; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 32, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "r#\"…\"# は引用符を含み、# の数が合うまで閉じない",
			src:  "let s = r#\"a \"b\" // 中\"#; // 外\n",
			want: []want{{line: 1, endLine: 1, col: 28, kind: KindLine, text: "// 外"}},
		},
		{
			name: "r##\"…\"## は \"# では閉じない",
			src:  "let s = r##\"a \"# // 中\"##; // 外\n",
			want: []want{{line: 1, endLine: 1, col: 29, kind: KindLine, text: "// 外"}},
		},
		{
			name: "生文字列は改行を含み、その中の // は無視され、行番号は進む",
			src: "let s = r#\"\n" +
				"// 生文字列の中\n" +
				"\"#;\n" +
				"// 本物\n",
			want: []want{{line: 4, endLine: 4, col: 1, kind: KindLine, text: "// 本物"}},
		},
		{
			name: "生文字列にエスケープは無い（\\\" では閉じない、と誤らない）",
			src:  "let s = r\"\\\"; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 15, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "バイト列 b\"…\" と生バイト列 br#\"…\"#",
			src:  "let a = b\"// 中\"; let b = br#\"// 中\"#; // 外\n",
			want: []want{{line: 1, endLine: 1, col: 42, kind: KindLine, text: "// 外"}},
		},
		{
			name: "ライフタイムは文字リテラルではない（行末のコメントを飲み込まない）",
			src:  "fn f<'a>(x: &'a str) -> &'a str { x } // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 39, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "ループのラベルも文字リテラルではない",
			src:  "'outer: loop { break 'outer; } // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 32, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "文字リテラルの中の引用符とエスケープ",
			src:  "let c = '\\''; let d = '\"'; // 後ろ\n",
			want: []want{{line: 1, endLine: 1, col: 28, kind: KindLine, text: "// 後ろ"}},
		},
		{
			name: "文字列は改行を含められる",
			src: "let s = \"\n" +
				"// 文字列の中\n" +
				"\";\n" +
				"// 本物\n",
			want: []want{{line: 4, endLine: 4, col: 1, kind: KindLine, text: "// 本物"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comments(Scan([]byte(tt.src), RustSpec()))
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

func TestSpecForRust(t *testing.T) {
	spec, ok := SpecFor("src/main.rs")
	if !ok || spec.Name != "rust" {
		t.Fatalf("SpecFor(.rs) = %q, %v; want rust, true", spec.Name, ok)
	}
}
