package place

import (
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// TestClassifyJS は、JS の位置クラスを押さえる。TS との差は型の語彙で、JS では type / interface /
// enum が識別子として使える。それらを宣言のキーワードに数えると、ただの代入の直前にある
// 自由コメントが doc を名乗り、書式の検査が誤爆する。
func TestClassifyJS(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "トップレベルの宣言の直前は doc。宣言名を取り出す",
			src: "/** open は開く */\n" +
				"export function open() {}\n",
			want: []want{{line: 1, endLine: 1, place: Doc, decl: "open", text: "/** open は開く */"}},
		},
		{
			name: "class のメソッドの直前も doc",
			src: "export class Scanner {\n" +
				"  // scan は字句に分解する。\n" +
				"  scan(src) {}\n" +
				"}\n",
			want: []want{{line: 2, endLine: 2, place: Doc, decl: "scan", text: "// scan は字句に分解する。"}},
		},
		{
			name: "type は識別子。その代入の直前は doc ではなく leading",
			src: "let type;\n" +
				"\n" +
				"// ここは以前 kind と呼んでいた。\n" +
				"type = \"line\";\n",
			want: []want{{line: 3, endLine: 3, place: Leading, text: "// ここは以前 kind と呼んでいた。"}},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.src, scan.JSSpec(), tt.want)
		})
	}
}
