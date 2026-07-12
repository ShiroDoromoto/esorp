package place

import (
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// TestClassifyRust は、doc 専用記法を持つ言語の判定を押さえる。字句だけで doc と分かるので
// 位置判定より kind を優先し、判定そのものは Go と同じ（違うのは LangSpec が持つ語彙だけ）。
func TestClassifyRust(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "内側 doc（//!）はファイル冒頭でも header ではなく doc。囲むものを説明するので宣言に紐づかない",
			src: "//! このモジュールは何かをする\n" +
				"\n" +
				"pub fn open() {}\n",
			want: []want{{line: 1, endLine: 1, place: Doc, decl: "", text: "//! このモジュールは何かをする"}},
		},
		{
			name: "外側 doc（///）は直後の宣言に紐づき、名前を取り出す",
			src: "/// open は開く\n" +
				"pub fn open() {}\n",
			want: []want{{line: 1, endLine: 1, place: Doc, decl: "open", text: "/// open は開く"}},
		},
		{
			name: "可視性の括弧は宣言の一部（pub(crate) fn）",
			src: "/// open は開く\n" +
				"pub(crate) fn open() {}\n",
			want: []want{{line: 1, endLine: 1, place: Doc, decl: "open", text: "/// open は開く"}},
		},
		{
			name: "属性は宣言の一部。属性の直前のコメントも doc",
			src: "/// Token は1つの字句\n" +
				"#[derive(Debug, Clone)]\n" +
				"pub struct Token {}\n",
			want: []want{{line: 1, endLine: 1, place: Doc, decl: "Token", text: "/// Token は1つの字句"}},
		},
		{
			name: "属性が重なっても宣言に届く",
			src: "use std::fmt;\n" +
				"\n" +
				"// scan は字句に分解する\n" +
				"#[cfg(test)]\n" +
				"#[allow(dead_code)]\n" +
				"fn scan() {}\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "scan", text: "// scan は字句に分解する"}},
		},
		{
			name: "struct のフィールドの直前は doc（type-like スコープ）",
			src: "pub struct Token {\n" +
				"    /// line は 1 始まり\n" +
				"    pub line: usize,\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Doc, decl: "line", text: "/// line は 1 始まり"}},
		},
		{
			name: "impl の中のメソッドの直前も doc",
			src: "impl Scanner {\n" +
				"    /// scan は字句に分解する\n" +
				"    pub fn scan(&self) {}\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Doc, decl: "scan", text: "/// scan は字句に分解する"}},
		},
		{
			name: "関数本体の中は、doc 記法でなければ leading（文の直前は逃げ場にならない）",
			src: "fn f() {\n" +
				"    // 以前はここで前方移行していた\n" +
				"    let x = 1;\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Leading, text: "// 以前はここで前方移行していた"}},
		},
		{
			name: "ライフタイムを挟んでも行末コメントは trailing のまま",
			src:  "fn f<'a>(x: &'a str) -> &'a str { x } // SAFETY: 何か\n",
			want: []want{{line: 1, endLine: 1, place: Trailing, text: "// SAFETY: 何か"}},
		},
		{
			name: "生文字列の中の // は器を作らない",
			src:  "const U: &str = r#\"http://example.com\"#;\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.src, scan.RustSpec(), tt.want)
		})
	}
}
