package place

import (
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// TestClassifyTS は、TS の位置クラスを押さえる。doc 記法は JSDoc だけなので、Go と同じく
// 「宣言の直前の //」も doc になる（位置を見ないと器が決まらない）。
func TestClassifyTS(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "トップレベルの宣言の直前は doc。宣言名を取り出す",
			src: "import { x } from \"./x\";\n" +
				"\n" +
				"/** open は開く */\n" +
				"export function open() {}\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "open", text: "/** open は開く */"}},
		},
		{
			name: "デコレータは宣言の一部。その直前のコメントも doc",
			src: "import { Injectable } from \"./di\";\n" +
				"\n" +
				"// Store は状態を持つ。\n" +
				"@Injectable()\n" +
				"export class Store {}\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "Store", text: "// Store は状態を持つ。"}},
		},
		{
			name: "interface のメンバの直前は doc（type-like スコープ）",
			src: "export interface Token {\n" +
				"  /** line は 1 始まり。 */\n" +
				"  line: number;\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Doc, decl: "line", text: "/** line は 1 始まり。 */"}},
		},
		{
			name: "class のメソッドの直前も doc",
			src: "export class Scanner {\n" +
				"  // scan は字句に分解する。\n" +
				"  scan(src: string) {}\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Doc, decl: "scan", text: "// scan は字句に分解する。"}},
		},
		{
			name: "関数本体の中は、宣言の直前でも doc にならず leading",
			src: "export function f() {\n" +
				"  // 以前はここで前方移行していた。\n" +
				"  const x = 1;\n" +
				"  return x;\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Leading, text: "// 以前はここで前方移行していた。"}},
		},
		{
			name: "メソッド本体の中も doc にならない（class の直下ではない）",
			src: "export class Scanner {\n" +
				"  scan() {\n" +
				"    // メモ。\n" +
				"    const x = 1;\n" +
				"    return x;\n" +
				"  }\n" +
				"}\n",
			want: []want{{line: 3, endLine: 3, place: Leading, text: "// メモ。"}},
		},
		{
			name: "テンプレートリテラルの地の文は器を作らない",
			src:  "const u = `http://example.com`;\n",
			want: nil,
		},
		{
			name: "補間の中のコメントは器になる（オブジェクトリテラルと同じく block スコープ）",
			src: "const s = `a${\n" +
				"  // 中のメモ。\n" +
				"  x\n" +
				"}b`;\n",
			want: []want{{line: 2, endLine: 2, place: Leading, text: "// 中のメモ。"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.src, scan.TSSpec(), tt.want)
		})
	}
}
