package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
      - place: doc
      - place: trailing
        label: ["TODO:"]
disposition:
  place-not-allowed: |
    この位置のコメントは許可されていません。
`

// testSource は、器が1つずつ現れるソース。header / doc は適合し、leading / orphan / ラベル無しの
// trailing は違反する。
const testSource = `// ファイル冒頭。
package p

// F は何かをする。
func F() {
	// 文の直前（leading）。
	x := 1
	x++ // ラベルの無い行末（trailing）。

	// 宙に浮いたコメント（orphan）。
}
`

// tree は、設定とソースを1つ置いた使い捨てのツリーを作り、設定ファイルの場所を返す。
func tree(t *testing.T, cfg, src string) string {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "esorp.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if src != "" {
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cfgPath
}

func TestRunExitCodes(t *testing.T) {
	clean := tree(t, testConfig, "// ファイル冒頭。\npackage p\n")
	dirty := tree(t, testConfig, testSource)
	broken := tree(t, "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n", "")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"引数なしは使い方を出して設定エラー", nil, exitConfig},
		{"help は適合", []string{"help"}, exitOK},
		{"未知のサブコマンドは設定エラー", []string{"nope"}, exitConfig},
		{"check の未知のフラグは設定エラー", []string{"check", "--nope"}, exitConfig},
		{"check の余分な引数は設定エラー", []string{"check", "--config", clean, "src"}, exitConfig},
		{"未知の --format は設定エラー", []string{"check", "--config", clean, "--format", "xml"}, exitConfig},
		{"設定が無ければ設定エラー", []string{"check", "--config", "無い.yaml"}, exitConfig},
		{"スキーマ違反は設定エラー", []string{"check", "--config", broken}, exitConfig},
		{"違反の無いツリーは適合", []string{"check", "--config", clean}, exitOK},
		{"違反のあるツリーは違反あり", []string{"check", "--config", dirty}, exitViolated},
		{"json 出力でも終了コードは同じ", []string{"check", "--config", dirty, "--format", "json"}, exitViolated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := run(tt.args, io.Discard, io.Discard)
			if got != tt.want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunHelpPrintsUsageToStdout(t *testing.T) {
	var stdout strings.Builder
	if got := run([]string{"help"}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("run(help) = %d, want %d", got, exitOK)
	}
	if !strings.Contains(stdout.String(), "使い方:") {
		t.Errorf("help が使い方を出していない: %q", stdout.String())
	}
}

func TestCheckTextOutput(t *testing.T) {
	var stdout strings.Builder
	if got := run([]string{"check", "--config", tree(t, testConfig, testSource)}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("run(check) = %d, want %d", got, exitViolated)
	}

	out := stdout.String()
	for _, want := range []string{
		"a.go:6:2  place-not-allowed  place=leading kind=line",
		"a.go:8:6  label-required  place=trailing kind=line",
		"a.go:10:2  place-not-allowed  place=orphan kind=line",
		"  この位置のコメントは許可されていません。",
		"3 件の違反",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
}

func TestCheckJSONOutput(t *testing.T) {
	var stdout strings.Builder
	if got := run([]string{"check", "--config", tree(t, testConfig, testSource), "--format", "json"}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("run(check --format json) = %d, want %d", got, exitViolated)
	}

	var got struct {
		Version int `json:"version"`
		Summary struct {
			Files      int `json:"files"`
			Violations int `json:"violations"`
		} `json:"summary"`
		Violations []struct {
			Path  string `json:"path"`
			Line  int    `json:"line"`
			ID    string `json:"id"`
			Place string `json:"place"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, stdout.String())
	}

	if got.Version != 1 || got.Summary.Files != 1 || got.Summary.Violations != 3 {
		t.Errorf("summary が違う: %+v", got.Summary)
	}
	if len(got.Violations) != 3 {
		t.Fatalf("違反が %d 件（3 件のはず）: %+v", len(got.Violations), got.Violations)
	}
	if v := got.Violations[0]; v.Path != "a.go" || v.Line != 6 || v.ID != "place-not-allowed" || v.Place != "leading" {
		t.Errorf("1件目が違う: %+v", v)
	}
}

// TestBaselineUpdateThenCheck は、baseline に載せた違反が check から消えて CI が緑になり、載せた
// コメントの本文を撫でるとキーが変わって違反として戻ってくることを確かめる（触ったなら、あなたが
// そのコメントの持ち主になる）。--allow-new を付けなければ、新しい違反は1件も載らない（ラチェット）。
func TestBaselineUpdateThenCheck(t *testing.T) {
	cfgPath := tree(t, testConfig+"baseline: .esorp-baseline.json\n", testSource)
	src := filepath.Join(filepath.Dir(cfgPath), "a.go")

	if got := run([]string{"check", "--config", cfgPath}, io.Discard, io.Discard); got != exitViolated {
		t.Fatalf("baseline に載せる前は違反あり: %d", got)
	}

	if got := run([]string{"baseline", "update", "--config", cfgPath}, io.Discard, io.Discard); got != exitOK {
		t.Fatalf("baseline update = %d", got)
	}
	if got := run([]string{"check", "--config", cfgPath}, io.Discard, io.Discard); got != exitViolated {
		t.Fatalf("--allow-new 無しで新しい違反が載ってしまっている: %d", got)
	}

	if got := run([]string{"baseline", "update", "--allow-new", "--config", cfgPath}, io.Discard, io.Discard); got != exitOK {
		t.Fatalf("baseline update --allow-new = %d", got)
	}

	var stdout strings.Builder
	if got := run([]string{"check", "--config", cfgPath}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("baseline に載せた後は適合のはず: %d\n%s", got, stdout.String())
	}
	if !strings.Contains(stdout.String(), "baseline が 3 件を抑えています") {
		t.Errorf("抑えた件数を告げていない: %q", stdout.String())
	}

	touched := strings.Replace(testSource, "// 文の直前（leading）。", "// 文の直前（leading）。少し足す。", 1)
	if err := os.WriteFile(src, []byte(touched), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{"check", "--config", cfgPath}, io.Discard, io.Discard); got != exitViolated {
		t.Fatal("baseline に載せたコメントを編集したのに、違反として戻ってきていない")
	}
}

// TestCheckWarnsOnUnscannableFiles は、字句を持たない言語のファイルについて、検査していないことを
// 告げるのを確かめる（黙って適合にしない）。
func TestCheckWarnsOnUnscannableFiles(t *testing.T) {
	cfgPath := tree(t, "syntax:\n  cstyle:\n    files: [\"**/*.tsx\"]\n    mode: structural\n", "")
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "a.tsx"), []byte("// x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if got := run([]string{"check", "--config", cfgPath}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(check) = %d, want %d", got, exitOK)
	}
	if !strings.Contains(stderr.String(), "a.tsx") {
		t.Errorf("読めなかったファイルを告げていない: %q", stderr.String())
	}
}

// gitTree は、設定とソースを1つコミットした使い捨ての git リポジトリを作り、設定ファイルの
// 場所を返す。--diff は git に依存するので、ここだけは本物の git を回す。
func gitTree(t *testing.T, cfg, src string) string {
	t.Helper()
	cfgPath := tree(t, cfg, src)
	dir := filepath.Dir(cfgPath)

	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "最初のコミット"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return cfgPath
}

// TestCheckDiff は、--diff が変更行に重なるコメントだけを見ることを確かめる。既にあった違反は、
// 同じファイルを触っても出てこない。
func TestCheckDiff(t *testing.T) {
	const committed = `// ファイル冒頭。
package p

// F は何かをする。
func F() {
	// 文の直前（leading）。既にある違反。
	x := 1
	_ = x
}
`
	cfgPath := gitTree(t, testConfig, committed)

	added := committed + "\n// 宙に浮いたコメント（orphan）。新しい違反。\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "a.go"), []byte(added), 0o600); err != nil {
		t.Fatal(err)
	}

	var full strings.Builder
	if got := run([]string{"check", "--config", cfgPath}, &full, io.Discard); got != exitViolated {
		t.Fatalf("check = %d, want %d", got, exitViolated)
	}
	if !strings.Contains(full.String(), "2 件の違反") {
		t.Fatalf("ツリー全体なら 2 件のはず:\n%s", full.String())
	}

	var only strings.Builder
	if got := run([]string{"check", "--config", cfgPath, "--diff", "HEAD"}, &only, io.Discard); got != exitViolated {
		t.Fatalf("check --diff = %d, want %d", got, exitViolated)
	}
	out := only.String()
	if !strings.Contains(out, "1 件の違反") || !strings.Contains(out, "place=orphan") {
		t.Errorf("--diff が変更行に絞れていない:\n%s", out)
	}
	if strings.Contains(out, "place=leading") {
		t.Errorf("--diff が既にある違反を拾っている:\n%s", out)
	}
}

// TestCheckDiffNoChange は、変更行に重なるコメントが無ければ --diff が適合になることを確かめる。
func TestCheckDiffNoChange(t *testing.T) {
	cfgPath := gitTree(t, testConfig, testSource)

	var stdout strings.Builder
	if got := run([]string{"check", "--config", cfgPath, "--diff", "HEAD"}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("check --diff = %d, want %d（変更が無ければ適合）\n%s", got, exitOK, stdout.String())
	}
}

// TestCheckDiffBadRef は、解決できない <ref> が設定エラーになることを確かめる（黙って全部を通さない）。
func TestCheckDiffBadRef(t *testing.T) {
	cfgPath := gitTree(t, testConfig, testSource)

	var stderr strings.Builder
	if got := run([]string{"check", "--config", cfgPath, "--diff", "存在しない参照"}, io.Discard, &stderr); got != exitConfig {
		t.Fatalf("check --diff 存在しない参照 = %d, want %d", got, exitConfig)
	}
	if !strings.Contains(stderr.String(), "分岐点") {
		t.Errorf("何が起きたか告げていない: %q", stderr.String())
	}
}

// TestCheckDiffTooManyArgs は、<ref> を2つ以上渡すのが使い方の誤りであることを確かめる。
func TestCheckDiffTooManyArgs(t *testing.T) {
	cfgPath := gitTree(t, testConfig, testSource)
	if got := run([]string{"check", "--config", cfgPath, "--diff", "HEAD", "余分"}, io.Discard, io.Discard); got != exitConfig {
		t.Errorf("余分な引数 = %d, want %d", got, exitConfig)
	}
}

// TestCheckDiffUntrackedFile は、追跡していない新しいファイルを --diff が丸ごと見ることを
// 確かめる（素通りさせない）。
func TestCheckDiffUntrackedFile(t *testing.T) {
	cfgPath := gitTree(t, testConfig, "// ファイル冒頭。\npackage p\n")
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "b.go"), []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	if got := run([]string{"check", "--config", cfgPath, "--diff", "HEAD"}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("check --diff = %d, want %d\n%s", got, exitViolated, stdout.String())
	}
	if !strings.Contains(stdout.String(), "3 件の違反") {
		t.Errorf("新しいファイルの違反を拾えていない:\n%s", stdout.String())
	}
}

// TestCheckRespectsGitignore は、respect_gitignore: true のとき、gitignore された場所を走査から
// 外すことを確かめる。git が「自分のコードではない」と宣言しているものを、esorp も自分のコードと
// して扱わない。
func TestCheckRespectsGitignore(t *testing.T) {
	cfgPath := gitTree(t, testConfig+"respect_gitignore: true\n", "// ファイル冒頭。\npackage p\n")
	dir := filepath.Dir(cfgPath)

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("vendor/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "v.go"), []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	if got := run([]string{"check", "--config", cfgPath}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("gitignore された vendor/ を見てしまっている: %d\n%s", got, stdout.String())
	}
}

// TestCheckIgnoresGitignoreWhenNotRepo は、git リポジトリでないツリーでも respect_gitignore: true が
// 壊れないことを確かめる（尊重できないだけで、走査は続く）。
func TestCheckIgnoresGitignoreWhenNotRepo(t *testing.T) {
	cfgPath := tree(t, testConfig+"respect_gitignore: true\n", testSource)
	if got := run([]string{"check", "--config", cfgPath}, io.Discard, io.Discard); got != exitViolated {
		t.Errorf("git の無いツリーで走査が止まっている: %d", got)
	}
}

// TestCheckWithoutRespectGitignore は、respect_gitignore を書かなければ gitignore された場所も
// 走査することを確かめる（設定に書かれていないものは効かない）。
func TestCheckWithoutRespectGitignore(t *testing.T) {
	cfgPath := gitTree(t, testConfig, "// ファイル冒頭。\npackage p\n")
	dir := filepath.Dir(cfgPath)

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("vendor/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor", "v.go"), []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := run([]string{"check", "--config", cfgPath}, io.Discard, io.Discard); got != exitViolated {
		t.Errorf("respect_gitignore 無しで vendor/ が落ちている: %d", got)
	}
}
