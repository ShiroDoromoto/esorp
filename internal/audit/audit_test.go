package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
)

// write は、走査させるツリーにファイルを1つ置く。
func write(t *testing.T, root, path, body string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run は、置いた設定でツリーを走査する。
func run(t *testing.T, root string) *Result {
	t.Helper()

	cfg, err := config.Load(filepath.Join(root, "esorp.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// ids は、上がってきた違反の id を出た順に並べる。
func ids(res *Result) []string {
	out := make([]string, 0, len(res.Findings))
	for _, f := range res.Findings {
		out = append(out, f.ID)
	}
	return out
}

// TestRunExcludes は、除外したファイルを読まないことを、実際にツリーを歩いて確かめる。どのファイルも
// 許可しない器（leading）を1つ持つので、読まれたファイルだけが違反として上がってくる。
func TestRunExcludes(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string) { write(t, root, path, body) }

	src := "package p\n\nfunc F() {\n\t// 文の直前。\n\tx := 1\n\t_ = x\n}\n"
	write("esorp.yaml", "syntax:\n  cstyle:\n    files: [\"**/*.go\", \"!vendor/**\", \"!**/*_gen.go\"]\n    mode: structural\n    allow:\n      - place: header\n")
	write("a.go", src)
	write("a_gen.go", src)
	write("vendor/lib/b.go", src)

	res := run(t, root)
	if res.Files != 1 {
		t.Errorf("読んだファイル = %d, want 1（vendor と *_gen.go は読まない）", res.Files)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("違反 = %d 件, want 1\n%#v", len(res.Findings), res.Findings)
	}
	if got := res.Findings[0].Path; got != "a.go" {
		t.Errorf("違反の出どころ = %q, want a.go", got)
	}
}

// historyRule は、層2 のルールを1つ持つ設定の断片。ツールは既定のルールを持たないので、こうして
// 設定に書いたときだけ層2 が効く。
const historyRule = `
rules:
  - id: no-history
    pattern: "かつて|no longer"
    message: 変化を語っています。今のコードの説明に書き直してください。
`

// TestRunLexiconOrder は、器 → 書式 → 語彙 の順で、先に落ちたら後は見ないことを確かめる。3つの
// ファイルは、いずれも3層すべてに反する同じ本文を持ち、器・書式・語彙のどこで初めて落ちるかだけが
// 違う。上がるのは、最初に落ちた1件だけであること。
func TestRunLexiconOrder(t *testing.T) {
	root := t.TempDir()
	write(t, root, "esorp.yaml", "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: doc\n        form:\n          headings: deny\n"+historyRule)

	write(t, root, "a.go", "package p\n\nfunc F() {\n\t// # 見出し。かつてはこうだった。\n\tx := 1\n\t_ = x\n}\n")
	write(t, root, "b.go", "package p\n\n// # 見出し。かつてはこうだった。\nfunc G() {}\n")
	write(t, root, "c.go", "package p\n\n// H はかつて同期だった。\nfunc H() {}\n")

	res := run(t, root)
	for _, tt := range []struct {
		path string
		want string
	}{
		{"a.go", "place-not-allowed"},
		{"b.go", "form-headings"},
		{"c.go", "no-history"},
	} {
		var got []string
		for _, f := range res.Findings {
			if f.Path == tt.path {
				got = append(got, f.ID)
			}
		}
		if len(got) != 1 || got[0] != tt.want {
			t.Errorf("%s の違反 = %v, want [%s]（先に落ちたら後は見ない）", tt.path, got, tt.want)
		}
	}
}

// TestRunContentOnly は、mode: content-only が層1 を飛ばして語彙だけを見ることを確かめる。
// 器の概念が無いファイルのための mode なので、どの位置のコメントも器では落ちない。
func TestRunContentOnly(t *testing.T) {
	root := t.TempDir()
	write(t, root, "esorp.yaml", "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: content-only\n"+historyRule)
	write(t, root, "a.go", "package p\n\nfunc F() {\n\t// かつてはこうだった。\n\tx := 1\n\t_ = x\n}\n")

	res := run(t, root)
	if got := ids(res); len(got) != 1 || got[0] != "no-history" {
		t.Errorf("違反 = %v, want [no-history]（器は問わず、語彙だけを見る）", got)
	}
}

// TestRunContentOnlyFamilies は、器の概念が無いファイル（hash / sgml / cssblock）が、拡張子を持つ
// ものも持たないものも、字句を引けて語彙まで届くことを確かめる。文字列の中の記号（"# …"）や、
// gitignore のパターンの一部の「#」がコメントに化けないことも、ここで押さえる。
func TestRunContentOnlyFamilies(t *testing.T) {
	root := t.TempDir()
	write(t, root, "esorp.yaml", "syntax:\n"+
		"  hash:\n    files: [\"**/*.yml\", \"**/*.sh\", \"Makefile\", \"**/.gitignore\"]\n    mode: content-only\n"+
		"  sgml:\n    files: [\"**/*.md\"]\n    mode: content-only\n"+
		"  cssblock:\n    files: [\"**/*.css\"]\n    mode: content-only\n"+historyRule)

	write(t, root, "ci.yml", "run: |\n  # かつてはここでビルドしていた（ブロックスカラーの中＝文字列）\nkey: 1  # かつてはこうだった\n")
	write(t, root, "run.sh", "echo \"# かつて\"  # かつてはこうだった\n")
	write(t, root, "Makefile", "build:\n\tgo build ./...  # かつてはこうだった\n")
	write(t, root, ".gitignore", "# かつてはこうだった\nfoo#かつて\n")
	write(t, root, "doc.md", "<!-- かつてはこうだった -->\n\nかつての散文はコメントではない。\n")
	write(t, root, "site.css", "a { color: #fff; }\n/* かつてはこうだった */\n")

	res := run(t, root)
	if len(res.Skipped) > 0 {
		t.Errorf("字句を引けなかったファイル = %v", res.Skipped)
	}
	if got := ids(res); len(got) != 6 {
		t.Errorf("違反 = %v（%#v）, want 6 ファイルに1件ずつ", got, res.Findings)
	}
}

// TestRunLexiconWherePath は、where.path が files: と同じ照合（! 始まりで除外）を通ることを確かめる。
func TestRunLexiconWherePath(t *testing.T) {
	root := t.TempDir()
	write(t, root, "esorp.yaml", "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: doc\n"+
		"rules:\n  - id: no-history\n    pattern: \"かつて\"\n    message: 変化を語っています。\n    where:\n      path: [\"**/*.go\", \"!internal/**\"]\n")

	src := "package p\n\n// F はかつて同期だった。\nfunc F() {}\n"
	write(t, root, "a.go", src)
	write(t, root, "internal/b.go", src)

	res := run(t, root)
	if len(res.Findings) != 1 || res.Findings[0].Path != "a.go" {
		t.Errorf("違反 = %#v, want a.go の1件（internal/ は where.path の除外で外れる）", res.Findings)
	}
}
