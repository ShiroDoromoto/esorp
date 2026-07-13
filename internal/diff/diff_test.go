package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sample = `diff --git a/a.go b/a.go
index 1111111..2222222 100644
--- a/a.go
+++ b/a.go
@@ -3,0 +4 @@ package p
+// 1行の追加。
@@ -10,2 +11,3 @@ func F() {
+// 3行の
+// 追加。
+x := 1
@@ -20 +22,0 @@ func G() {
-// 削除だけ。
diff --git a/b.go b/b.go
deleted file mode 100644
index 3333333..0000000
--- a/b.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package p
diff --git a/c.go b/c.go
new file mode 100644
index 0000000..4444444
--- /dev/null
+++ b/c.go
@@ -0,0 +1,2 @@
+package p
+
`

func TestParse(t *testing.T) {
	got, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}

	want := Ranges{
		"a.go": {{From: 4, To: 4}, {From: 11, To: 13}},
		"c.go": {{From: 1, To: 2}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n got %v\nwant %v", got, want)
	}
}

// TestParseNoAddedLines は、追加行を持たない差分が空になることを確かめる。削除だけのハンク
// （+n,0）にも、削除されたファイル（+++ /dev/null）にも、監査するものが無い。
func TestParseNoAddedLines(t *testing.T) {
	got, err := Parse(strings.NewReader(`--- a/b.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package p
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("Parse: %v、削除だけの差分は空であること", got)
	}
}

func TestParseBadHunk(t *testing.T) {
	_, err := Parse(strings.NewReader("+++ b/a.go\n@@ -1 @@\n"))
	if err == nil {
		t.Error("壊れたハンクヘッダを読めてしまいました")
	}
}

// TestChangedShallow は、浅いクローンで分岐点が取れないときに、その理由を告げることを見る。
// depth 1 で取った枝に、比較先を浅いまま足す形（CI の checkout の既定）を再現する。
func TestChangedShallow(t *testing.T) {
	origin := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(origin, "init", "-q", "-b", "main")
	write(origin, "a.go", "package p\n")
	git(origin, "add", "-A")
	git(origin, "commit", "-q", "-m", "main")
	git(origin, "checkout", "-q", "-b", "feature")
	write(origin, "b.go", "package p\n")
	git(origin, "add", "-A")
	git(origin, "commit", "-q", "-m", "feature")

	work := filepath.Join(t.TempDir(), "work")
	git(t.TempDir(), "clone", "-q", "--depth", "1", "--branch", "feature", "file://"+origin, work)
	git(work, "fetch", "-q", "--no-tags", "origin", "+refs/heads/main:refs/remotes/origin/main")

	_, err := Changed(work, "origin/main")
	if err == nil {
		t.Fatal("浅いクローンで分岐点が取れてしまいました")
	}
	if !strings.Contains(err.Error(), "浅いクローン") {
		t.Errorf("浅いことを告げていません: %v", err)
	}
}

func TestOverlaps(t *testing.T) {
	r := Ranges{"a.go": {{From: 10, To: 12}}}

	cases := []struct {
		name     string
		path     string
		from, to int
		want     bool
	}{
		{"手前で終わる", "a.go", 1, 9, false},
		{"下端に触れる", "a.go", 1, 10, true},
		{"すっぽり入る", "a.go", 11, 11, true},
		{"またぐ", "a.go", 1, 20, true},
		{"上端に触れる", "a.go", 12, 20, true},
		{"後ろで始まる", "a.go", 13, 20, false},
		{"別のファイル", "b.go", 10, 12, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.Overlaps(c.path, c.from, c.to); got != c.want {
				t.Errorf("Overlaps(%q, %d, %d) = %v, want %v", c.path, c.from, c.to, got, c.want)
			}
		})
	}
}
