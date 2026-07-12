package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
)

// TestSelects は、files: の glob がパスを選ぶかを押さえる。除外（! 始まり）はいつも勝つので、
// 並べる順は結果を変えない。
func TestSelects(t *testing.T) {
	globs := []string{"**/*.go", "!vendor/**", "!**/*_gen.go"}

	tests := []struct {
		path string
		want bool
	}{
		{"internal/scan/cstyle.go", true},
		{"vendor/x/y.go", false},
		{"internal/scan/table_gen.go", false},
		{"README.md", false},
		{"vendor", false},
	}
	for _, tt := range tests {
		if got := selects(globs, tt.path); got != tt.want {
			t.Errorf("selects(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}

	if !selects([]string{"!vendor/**", "**/*.go"}, "a.go") {
		t.Error("除外を先に書いたら、正の glob が効かなくなった")
	}
}

// TestRunExcludes は、除外したファイルを読まないことを、実際にツリーを歩いて確かめる。どのファイルも
// 許可しない器（leading）を1つ持つので、読まれたファイルだけが違反として上がってくる。
func TestRunExcludes(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	src := "package p\n\nfunc F() {\n\t// 文の直前。\n\tx := 1\n\t_ = x\n}\n"
	write("esorp.yaml", "syntax:\n  cstyle:\n    files: [\"**/*.go\", \"!vendor/**\", \"!**/*_gen.go\"]\n    mode: structural\n    allow:\n      - place: header\n")
	write("a.go", src)
	write("a_gen.go", src)
	write("vendor/lib/b.go", src)

	cfg, err := config.Load(filepath.Join(root, "esorp.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(cfg, root, nil)
	if err != nil {
		t.Fatal(err)
	}

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
