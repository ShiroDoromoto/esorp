package place

import (
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// TestClassifyTSX は、TSX の位置クラスを押さえる。JSX が増やす器は無い。タグの中身はテキストで
// あって器を作らず、JSX のコメント（{/* … */}）が置かれる { … } は、ただのブロックスコープなので、
// そこに doc は無い。つまり JSX の中に書けるのは、ラベル付きの行末コメントだけになる。
func TestClassifyTSX(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "JSX を返す関数の直前は doc。宣言名を取り出す",
			src: "import { h } from \"./h\";\n" +
				"\n" +
				"// App は画面を組み立てる。\n" +
				"export function App() {\n" +
				"  return <div>本文</div>;\n" +
				"}\n",
			want: []want{{line: 3, endLine: 3, place: Doc, decl: "App", text: "// App は画面を組み立てる。"}},
		},
		{
			name: "JSX テキストは器を作らない",
			src: "export function App() {\n" +
				"  return <p>詳しくは http://example.com</p>;\n" +
				"}\n",
			want: nil,
		},
		{
			name: "{/* … */} は式の開きと同じ行なので trailing（ラベルが要る）",
			src: "export function App() {\n" +
				"  return (\n" +
				"    <div>\n" +
				"      {/* TODO: 空のときの表示。 */}\n" +
				"    </div>\n" +
				"  );\n" +
				"}\n",
			want: []want{{line: 4, endLine: 4, place: Trailing, text: "/* TODO: 空のときの表示。 */"}},
		},
		{
			name: "式の中で行を落としたコメントは orphan",
			src: "export function App() {\n" +
				"  return (\n" +
				"    <div>\n" +
				"      {\n" +
				"        /* 2024-05 の改修で二重描画を直した。 */\n" +
				"      }\n" +
				"    </div>\n" +
				"  );\n" +
				"}\n",
			want: []want{{line: 5, endLine: 5, place: Orphan, text: "/* 2024-05 の改修で二重描画を直した。 */"}},
		},
		{
			name: "JSX の後ろの宣言も、器を見失わない",
			src: "export function App() {\n" +
				"  return <div>本文</div>;\n" +
				"}\n" +
				"\n" +
				"// Footer は末尾を組み立てる。\n" +
				"export function Footer() {\n" +
				"  return <>{null}</>;\n" +
				"}\n",
			want: []want{{line: 5, endLine: 5, place: Doc, decl: "Footer", text: "// Footer は末尾を組み立てる。"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, tt.src, scan.TSXSpec(), tt.want)
		})
	}
}
