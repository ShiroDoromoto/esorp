package scan

import "testing"

func TestBody(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"行コメントの記号と空白を剥がす", "//   SAFETY: 呼び出し側が保証する", "SAFETY: 呼び出し側が保証する"},
		{"塊は行ごとに剥がす", "// 1行目。\n// 2行目。", "1行目。\n2行目。"},
		{"ブロックコメントの記号を剥がす", "/* ラベル */", "ラベル"},
		{"複数行ブロックの継ぎ行の * と、記号だけの行を落とす", "/*\n * 1行目。\n * 2行目。\n */", "1行目。\n2行目。"},
		{"中の空行は段落の区切りなので残す", "// 1段落目。\n//\n// 2段落目。", "1段落目。\n\n2段落目。"},
		{"本文そのものには手を触れない", "// # 見出し", "# 見出し"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Body(tt.text, GoSpec()); got != tt.want {
				t.Errorf("Body(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestBodyDocNotation(t *testing.T) {
	spec := LangSpec{LineComment: "//", BlockOpen: "/*", BlockClose: "*/", DocLine: []string{"///", "//!"}, DocBlock: []string{"/**"}}
	if got := Body("/// open は開く", spec); got != "open は開く" {
		t.Errorf("docline = %q", got)
	}
	if got := Body("/** JSDoc */", spec); got != "JSDoc" {
		t.Errorf("docblock = %q", got)
	}
}

func TestUnwrap(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"英語の折り返しは空白でつなぐ", "G does not do this no\nlonger.", "G does not do this no longer."},
		{"日本語の折り返しは空白を挟まない", "F はかつ\nて同期だった。", "F はかつて同期だった。"},
		{"全角と半角の境目には空白を挟む", "本文は\nBody が作る。", "本文は Body が作る。"},
		{"長音で折り返しても空白を挟まない", "サーバ\nー。", "サーバー。"},
		{"約物で始まる行でも空白を挟まない", "これは\n「器」だ。", "これは「器」だ。"},
		{"段落の区切りは残す", "1段落目。\n\n2段落目。", "1段落目。\n2段落目。"},
		{"畳む先が無ければそのまま", "1行だけ。", "1行だけ。"},
		{"空の本文は空のまま", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Unwrap(tt.body); got != tt.want {
				t.Errorf("Unwrap(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

// TestUnwrapKeepsParagraphsApart は、畳んだ段落どうしが地続きにならないことを見る。地続きにすると、
// 段落をまたいだ句に当たってしまう。
func TestUnwrapKeepsParagraphsApart(t *testing.T) {
	if got := Unwrap(Body("// no\n//\n// longer", GoSpec())); got != "no\nlonger" {
		t.Errorf("Unwrap = %q, want %q", got, "no\nlonger")
	}
}
