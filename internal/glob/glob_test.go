package glob

import "testing"

// TestSelects は、glob の並びがパスを選ぶかを押さえる。除外（! 始まり）はいつも勝つので、
// 並べる順は結果を変えない。syntax.files: と rules[].where.path: の両方が、この照合を通る。
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
		if got := Selects(globs, tt.path); got != tt.want {
			t.Errorf("Selects(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}

	if !Selects([]string{"!vendor/**", "**/*.go"}, "a.go") {
		t.Error("除外を先に書いたら、正の glob が効かなくなった")
	}
	if Selects([]string{"!vendor/**"}, "a.go") {
		t.Error("除外だけの並びが、何かを選んだ")
	}
}

// TestExcluded は、除外だけを見る（走査で降りないディレクトリの判断が使う）。
func TestExcluded(t *testing.T) {
	globs := []string{"**/*.go", "!vendor/**"}

	if !Excluded(globs, "vendor/lib") {
		t.Error("除外に当たるディレクトリを、除外と見なかった")
	}
	if Excluded(globs, "internal") {
		t.Error("除外に当たらないディレクトリを、除外と見た")
	}
}
