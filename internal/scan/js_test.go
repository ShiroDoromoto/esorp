package scan

import "testing"

// TestCStyleJS は、JS の字句を押さえる。TS と同じ形（JSDoc・正規表現リテラル・テンプレート
// リテラル）が、型注釈の無いソースでも読めることを見る。
func TestCStyleJS(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "JSDoc は docblock",
			src: "/** open は開く */\n" +
				"export function open() {}\n",
			want: []want{{line: 1, endLine: 1, col: 1, kind: KindDocBlock, text: "/** open は開く */"}},
		},
		{
			name: "正規表現リテラルの中の引用符は文字列を開かない",
			src:  "const re = /it's/g; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 21, kind: KindLine, text: "// 実コメント"}},
		},
		{
			name: "テンプレートリテラルの地の文にある // はコメントではない",
			src:  "const u = `http://example.com`; // 実コメント\n",
			want: []want{{line: 1, endLine: 1, col: 33, kind: KindLine, text: "// 実コメント"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := comments(CStyle([]byte(tt.src), JSSpec()))
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

// TestSpecForJS は、.js 族と .jsx が別の字句を引くことを押さえる。.js に JSX を許すと、
// 除算とジェネリクスの「<」をタグの開きと読む恐れがある。JSX は .jsx でだけ有効にする。
func TestSpecForJS(t *testing.T) {
	for _, path := range []string{"build.js", "build.mjs", "build.cjs"} {
		spec, ok := SpecFor(path)
		if !ok || spec.Name != "javascript" || spec.JSX {
			t.Errorf("SpecFor(%q) = %q (JSX=%v), %v; want javascript (JSX=false), true", path, spec.Name, spec.JSX, ok)
		}
	}
	if spec, ok := SpecFor("src/App.jsx"); !ok || spec.Name != "jsx" || !spec.JSX {
		t.Errorf("SpecFor(.jsx) = %q (JSX=%v), %v; want jsx (JSX=true), true", spec.Name, spec.JSX, ok)
	}
}

// TestJSDocFences は、JSDoc がコードブロックをフェンスで書くことを押さえる。DocFences を
// 落とすと、フェンスの中身を散文の段落と数え、コードの行どうしを畳んでしまう。
func TestJSDocFences(t *testing.T) {
	if !JSSpec().DocFences {
		t.Error("JSSpec.DocFences = false; JSDoc のコードブロックはフェンス（```）で書く")
	}
}
