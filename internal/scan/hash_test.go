package scan

import "testing"

// checkComments は、得たコメントを期待値と突き合わせる。
func checkComments(t *testing.T, got []Token, wants []want) {
	t.Helper()

	if len(got) != len(wants) {
		t.Fatalf("コメント数 = %d, want %d\n得たもの: %#v", len(got), len(wants), got)
	}
	for i, w := range wants {
		g := got[i]
		if g.Kind != w.kind || g.Line != w.line || g.EndLine != w.endLine || g.Col != w.col || g.Text != w.text {
			t.Errorf("comment[%d] = {%v %d-%d:%d %q}, want {%v %d-%d:%d %q}",
				i, g.Kind, g.Line, g.EndLine, g.Col, g.Text,
				w.kind, w.line, w.endLine, w.col, w.text)
		}
	}
}

// TestScanHash は、hash ファミリの字句を押さえる。「#」は語の中にも現れるので、コメントを開くのは
// 行頭か空白の直後だけであり、引用符の中では開かない。
func TestScanHash(t *testing.T) {
	tests := []struct {
		name string
		spec LangSpec
		src  string
		want []want
	}{
		{
			name: "行コメントと行末コメント",
			spec: ShellSpec(),
			src:  "# 頭\nls -l  # 後ろ\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "# 頭"},
				{line: 2, endLine: 2, col: 8, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "語の中の # はコメントではない",
			spec: ShellSpec(),
			src:  "echo ${path#/usr}\n",
			want: nil,
		},
		{
			name: "引用符の中の # はコメントではない",
			spec: ShellSpec(),
			src:  "echo \"a # b\" '#c'\n",
			want: nil,
		},
		{
			name: "引用符は行をまたがない（閉じ忘れの先のコメントを飲み込まない）",
			spec: ShellSpec(),
			src:  "echo 'don't\n# 後の行\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# 後の行"},
			},
		},
		{
			name: "PowerShell のブロックコメント",
			spec: PowerShellSpec(),
			src:  "<#\n説明\n#>\nGet-Item  # 後ろ\n",
			want: []want{
				{line: 1, endLine: 3, col: 1, kind: KindBlock, text: "<#\n説明\n#>"},
				{line: 4, endLine: 4, col: 11, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "gitignore の # は行頭のみ",
			spec: GitignoreSpec(),
			src:  "# 無視するもの\n*.log\nfoo#bar\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "# 無視するもの"},
			},
		},
		{
			name: "Makefile のレシピ行の # はシェルのコメント",
			spec: MakeSpec(),
			src:  "build:\n\tgo build ./...  # 全部\n",
			want: []want{
				{line: 2, endLine: 2, col: 18, kind: KindLine, text: "# 全部"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), tt.spec)), tt.want)
		})
	}
}

// TestScanYAMLBlockScalar は、ブロックスカラー（| >）の中身がコメントにならないことを押さえる。
// 中身は文字列であって、そこに現れる「#」はコメント記号ではない。
func TestScanYAMLBlockScalar(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "ブロックスカラーの中の # は拾わない",
			src:  "script: |\n  echo hi\n  # これは文字列\nkey: 1  # これはコメント\n",
			want: []want{
				{line: 4, endLine: 4, col: 9, kind: KindLine, text: "# これはコメント"},
			},
		},
		{
			name: "見出しの後ろのコメントは拾う",
			src:  "script: |  # 見出しの後ろ\n  # 中身\n",
			want: []want{
				{line: 1, endLine: 1, col: 12, kind: KindLine, text: "# 見出しの後ろ"},
			},
		},
		{
			name: "並びの印の下のブロックスカラー",
			src:  "steps:\n  - run: >-\n      # 中身\n  - run: echo  # 後ろ\n",
			want: []want{
				{line: 4, endLine: 4, col: 16, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "字下げが戻ればブロックスカラーは終わる",
			src:  "a: |\n  中身\n\nb: 1  # 後ろ\n",
			want: []want{
				{line: 4, endLine: 4, col: 7, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "値として現れた | は見出しではない",
			src:  "cmd: echo |\n# コメント\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# コメント"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), YAMLSpec())), tt.want)
		})
	}
}

// TestScanHeredoc は、ヒアドキュメントの中身を文字列として読むことを押さえる。中身に現れる「#」は
// コメントではない。区切りが最後まで現れないもの（算術の左シフト）を飲み込まないことも、ここで押さえる。
func TestScanHeredoc(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "中身の # はコメントではない",
			src:  "cat <<EOF\n# これは文字列\nEOF\n# これはコメント\n",
			want: []want{
				{line: 4, endLine: 4, col: 1, kind: KindLine, text: "# これはコメント"},
			},
		},
		{
			name: "引用した区切りとタブ字下げ（<<-'EOF'）",
			src:  "\tcat <<-'EOF'\n\t# 文字列\n\tEOF\n\techo hi  # コメント\n",
			want: []want{
				{line: 4, endLine: 4, col: 11, kind: KindLine, text: "# コメント"},
			},
		},
		{
			name: "開きの行の後ろのコメントは拾う",
			src:  "cat <<EOF  # 後ろ\n# 文字列\nEOF\n",
			want: []want{
				{line: 1, endLine: 1, col: 12, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "1行で2つ開く",
			src:  "cat <<A <<B\n# 文字列\nA\n# 文字列\nB\n# コメント\n",
			want: []want{
				{line: 6, endLine: 6, col: 1, kind: KindLine, text: "# コメント"},
			},
		},
		{
			name: "区切りが現れなければ飲み込まない",
			src:  "n=$(( x << 2 ))\n# コメント\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# コメント"},
			},
		},
		{
			name: "閉じ忘れは飲み込まない",
			src:  "cat <<EOF\necho hi\n# コメント\n",
			want: []want{
				{line: 3, endLine: 3, col: 1, kind: KindLine, text: "# コメント"},
			},
		},
		{
			name: "here-string（<<<）は開きではない",
			src:  "grep x <<< \"$s\"\n# コメント\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# コメント"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), ShellSpec())), tt.want)
		})
	}
}

// TestScanHeredocBody は、ヒアドキュメントの中身が1つの文字列トークンになることを押さえる。
// コメントだけを見ていると、中身が「読み飛ばされた」のか「文字列として読まれた」のかが区別できない。
func TestScanHeredocBody(t *testing.T) {
	toks := Scan([]byte("cat <<EOF\n# 一行目\n二行目\nEOF\n"), ShellSpec())

	var got []Token
	for _, tk := range toks {
		if tk.Kind == KindString {
			got = append(got, tk)
		}
	}
	if len(got) != 1 {
		t.Fatalf("文字列トークン = %#v, want 1 件（中身まるごと）", got)
	}
	if want := "# 一行目\n二行目\n"; got[0].Text != want {
		t.Errorf("中身 = %q, want %q", got[0].Text, want)
	}
	if got[0].Line != 2 || got[0].EndLine != 3 {
		t.Errorf("行 = %d-%d, want 2-3", got[0].Line, got[0].EndLine)
	}
}

// TestScanSGML は、sgml ファミリ（HTML / SVG / Markdown）の字句を押さえる。コメントは <!-- --> だけで、
// 散文の引用符は文字列を開かない。
func TestScanSGML(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "HTML のコメント",
			src:  "<div>a</div>\n<!-- 説明 -->\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindBlock, text: "<!-- 説明 -->"},
			},
		},
		{
			name: "散文の引用符はコメントを飲み込まない",
			src:  "don't stop\n<!-- 説明 -->\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindBlock, text: "<!-- 説明 -->"},
			},
		},
		{
			name: "複数行のコメント",
			src:  "<!--\n説明\n-->\n",
			want: []want{
				{line: 1, endLine: 3, col: 1, kind: KindBlock, text: "<!--\n説明\n-->"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), SGMLSpec())), tt.want)
		})
	}
}

// TestScanCSS は、cssblock ファミリの字句を押さえる。行コメントは無く、16進の色（#fff）は
// コメントを開かない。
func TestScanCSS(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "16進の色はコメントではない",
			src:  "a { color: #fff; }  /* 説明 */\n",
			want: []want{
				{line: 1, endLine: 1, col: 21, kind: KindBlock, text: "/* 説明 */"},
			},
		},
		{
			name: "url() の中の /* はコメントではない",
			src:  "a { background: url(\"/*.png\"); }\n/* 説明 */\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindBlock, text: "/* 説明 */"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), CSSSpec())), tt.want)
		})
	}
}

// TestSpecForNames は、拡張子を持たないファイルの字句も名前から引けることを押さえる。
func TestSpecForNames(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "Makefile", want: "make"},
		{path: "build/Dockerfile", want: "dockerfile"},
		{path: ".gitignore", want: "gitignore"},
		{path: ".github/workflows/ci.yml", want: "yaml"},
		{path: "scripts/run.sh", want: "shell"},
		{path: "esorp.toml", want: "toml"},
		{path: "site/index.html", want: "sgml"},
		{path: "README.md", want: "sgml"},
		{path: "site/main.css", want: "css"},
		{path: "tool.ps1", want: "powershell"},
		{path: "data.json", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			spec, ok := SpecFor(tt.path)
			if ok != (tt.want != "") {
				t.Fatalf("SpecFor(%q) を引けたか = %v", tt.path, ok)
			}
			if spec.Name != tt.want {
				t.Errorf("SpecFor(%q) = %q, want %q", tt.path, spec.Name, tt.want)
			}
		})
	}
}

// TestScanShebang は、1行目の shebang がコメントとして読まれないことを押さえる。shebang は
// カーネルへの指示であって、本文ではない。これをコメントとして読むと、直後に置かれた冒頭の
// コメントと1つの器に繋がり、header をこれが占めてしまう。
func TestScanShebang(t *testing.T) {
	tests := []struct {
		name string
		spec LangSpec
		src  string
		want []want
	}{
		{
			name: "shebang はコメントにならず、直後のコメントとも繋がらない",
			spec: ShellSpec(),
			src:  "#!/bin/sh\n# 説明\nls\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# 説明"},
			},
		},
		{
			name: "#! と パスの間に空白があってもよい",
			spec: ShellSpec(),
			src:  "#! /usr/bin/env bash\n# 説明\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# 説明"},
			},
		},
		{
			name: "2行目以降の #! はただのコメント",
			spec: ShellSpec(),
			src:  "# 頭\n#!/bin/sh\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "# 頭"},
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "#!/bin/sh"},
			},
		},
		{
			name: "Rust の内側属性は shebang ではない（パスが続かない）",
			spec: RustSpec(),
			src:  "#![allow(dead_code)]\n//! クレートの説明\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindDocLine, text: "//! クレートの説明"},
			},
		},
		{
			name: "hash ファミリ以外の shebang も飲む（node の CLI）",
			spec: TSSpec(),
			src:  "#!/usr/bin/env node\n// 説明\nmain()\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "// 説明"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), tt.spec)), tt.want)
		})
	}
}

// TestShebangToken は、shebang が1行まるごと1つのトークンとして出ること、そしてそれがコメント
// でもコードでもないことを押さえる。
func TestShebangToken(t *testing.T) {
	toks := Scan([]byte("#!/bin/sh\nls\n"), ShellSpec())
	if len(toks) == 0 {
		t.Fatal("トークンが1つも無い")
	}
	got := toks[0]
	if got.Kind != KindShebang || got.Line != 1 || got.Col != 1 || got.Text != "#!/bin/sh" {
		t.Errorf("toks[0] = {%v %d:%d %q}, want {shebang 1:1 %q}", got.Kind, got.Line, got.Col, got.Text, "#!/bin/sh")
	}
	if got.Kind.IsComment() || got.Kind.IsCode() {
		t.Error("shebang はコメントでもコードでもない")
	}
}

// TestScanPowerShellHereString は、ヒアストリング（@" … "@）の中身を文字列として読むことを押さえる。
// 中身に現れる「#」はコメントではない。閉じるのは行頭に立つ「"@」だけで、行中のものでは閉じない。
func TestScanPowerShellHereString(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []want
	}{
		{
			name: "中身の # はコメントではない",
			src:  "$s = @\"\n# これは文字列\n\"@\n# これはコメント\n",
			want: []want{
				{line: 4, endLine: 4, col: 1, kind: KindLine, text: "# これはコメント"},
			},
		},
		{
			name: "変数を展開しない形（@' … '@）",
			src:  "$s = @'\n# 文字列\n'@\nGet-Item  # 後ろ\n",
			want: []want{
				{line: 4, endLine: 4, col: 11, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "開きの行の後ろにコメントは書けない（@\" の後は行末）",
			src:  "$h = @{ k = \"@x\" }  # 後ろ\n",
			want: []want{
				{line: 1, endLine: 1, col: 21, kind: KindLine, text: "# 後ろ"},
			},
		},
		{
			name: "行中の \"@ では閉じない",
			src:  "$s = @\"\nmail: a\"@b.example\n\"@\n# コメント\n",
			want: []want{
				{line: 4, endLine: 4, col: 1, kind: KindLine, text: "# コメント"},
			},
		},
		{
			name: "中身のブロックコメント記号もただの文字",
			src:  "$s = @\"\n<# 文字列 #>\n\"@\n<# 本物 #>\n",
			want: []want{
				{line: 4, endLine: 4, col: 1, kind: KindBlock, text: "<# 本物 #>"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), PowerShellSpec())), tt.want)
		})
	}
}

// TestScanPowerShellHereStringBody は、ヒアストリングの中身が1つの文字列トークンになることを
// 押さえる。コメントだけを見ていると、中身が「読み飛ばされた」のか「文字列として読まれた」のかが
// 区別できない。
func TestScanPowerShellHereStringBody(t *testing.T) {
	toks := Scan([]byte("$s = @\"\n# 一行目\n二行目\n\"@\n"), PowerShellSpec())

	var got []Token
	for _, tk := range toks {
		if tk.Kind == KindString {
			got = append(got, tk)
		}
	}
	if len(got) != 1 {
		t.Fatalf("文字列トークン = %#v, want 1 件（開きから閉じまで）", got)
	}
	if want := "@\"\n# 一行目\n二行目\n\"@"; got[0].Text != want {
		t.Errorf("中身 = %q, want %q", got[0].Text, want)
	}
	if got[0].Line != 1 || got[0].EndLine != 4 {
		t.Errorf("行 = %d-%d, want 1-4", got[0].Line, got[0].EndLine)
	}
}
