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
