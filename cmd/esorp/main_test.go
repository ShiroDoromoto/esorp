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
		"3 violations",
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

	if got.Version != 2 || got.Summary.Files != 1 || got.Summary.Violations != 3 {
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
	if !strings.Contains(stdout.String(), "baseline holds down 3") {
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
	cfgPath := tree(t, "syntax:\n  cstyle:\n    files: [\"**/*.py\"]\n    mode: structural\n", "")
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "a.py"), []byte("# x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if got := run([]string{"check", "--config", cfgPath}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(check) = %d, want %d", got, exitOK)
	}
	if !strings.Contains(stderr.String(), "a.py") {
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
	if !strings.Contains(full.String(), "2 violations") {
		t.Fatalf("ツリー全体なら 2 件のはず:\n%s", full.String())
	}

	var only strings.Builder
	if got := run([]string{"check", "--config", cfgPath, "--diff", "HEAD"}, &only, io.Discard); got != exitViolated {
		t.Fatalf("check --diff = %d, want %d", got, exitViolated)
	}
	out := only.String()
	if !strings.Contains(out, "1 violations") || !strings.Contains(out, "place=orphan") {
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
	if !strings.Contains(stdout.String(), "3 violations") {
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

// TestInitWritesUsableConfig は、生成した設定でそのまま check が回ることを確かめる。生成した
// 設定がその場で設定エラーになれば、最初の一歩で信頼を失う。
func TestInitWritesUsableConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "esorp.yaml")
	src := "// F は何かをする。\nfunc F() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if got := run([]string{"init", "--config", cfgPath}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(init) = %d, want %d\n%s", got, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "esorp.yaml") {
		t.Errorf("生成した場所を告げていない: %q", stdout.String())
	}

	stdout.Reset()
	if got := run([]string{"check", "--config", cfgPath}, &stdout, &stderr); got != exitOK {
		t.Fatalf("生成した設定で check = %d, want %d\n%s", got, exitOK, stdout.String())
	}
}

// TestInitGuidesBothFirstDayMoves は、init の出力が導入初日の営みを両方案内することを確かめる。
// 既存の違反を baseline に載せる（決定論の側）だけでは片手落ちで、層1・層2 を通り抜けた既存の
// コメントを層3 に回す口（esorp review）に触れなければ、そこへ辿り着く導線が生成直後に無い。
func TestInitGuidesBothFirstDayMoves(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "esorp.yaml")

	var stdout, stderr strings.Builder
	if got := run([]string{"init", "--config", cfgPath}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(init) = %d, want %d\n%s", got, exitOK, stderr.String())
	}
	for _, want := range []string{"esorp baseline update --allow-new", "esorp review"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("導線に %q が無い: %q", want, stdout.String())
		}
	}
}

// TestInitDoesNotOverwrite は、既にある設定を黙って上書きしないことを確かめる。生成された設定は
// その時点でユーザーのものであり、手を入れた分は戻ってこない。
func TestInitDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "esorp.yaml")
	mine := []byte("syntax: {}\n")
	if err := os.WriteFile(cfgPath, mine, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if got := run([]string{"init", "--config", cfgPath}, &stdout, &stderr); got != exitConfig {
		t.Fatalf("run(init) = %d, want %d（既にあるなら断る）", got, exitConfig)
	}
	if body, err := os.ReadFile(cfgPath); err != nil || string(body) != string(mine) {
		t.Fatalf("手元の設定が書き換えられた: %q", string(body))
	}
	if !strings.Contains(stderr.String(), "--force") {
		t.Errorf("上書きの手段を告げていない: %q", stderr.String())
	}

	if got := run([]string{"init", "--config", cfgPath, "--force"}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(init --force) = %d, want %d", got, exitOK)
	}
	if body, err := os.ReadFile(cfgPath); err != nil || string(body) == string(mine) {
		t.Fatal("--force なのに上書きされていない")
	}
}

// TestExplain は、explain が違反と、それを決めた設定の該当箇所を指すことを確かめる。違反を「禁止」
// とだけ伝えると、書き手は言い換えて再投稿する。何がその器を許していないのかまで見せて、はじめて直せる。
func TestExplain(t *testing.T) {
	cfgPath := tree(t, testConfig, testSource)

	tests := []struct {
		name   string
		target string
		want   []string
	}{
		{
			name:   "許されていない器は、許可されている器の列挙を指す",
			target: "a.go:6:2",
			want: []string{
				"a.go:6:2  place-not-allowed  place=leading kind=line",
				"この位置のコメントは許可されていません。",
				"の syntax.cstyle.allow です:",
				"allow[0]  place: header",
				"allow[2]  place: trailing  label: [TODO:]",
				"place: leading（kind: line）はこの列挙にありません",
			},
		},
		{
			name:   "ラベル必須は、そのラベルの列挙を指す",
			target: "a.go:8",
			want: []string{
				"a.go:8:6  label-required",
				"の syntax.cstyle.allow[2].label です:",
				"label: [TODO:]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout strings.Builder
			if got := run([]string{"explain", "--config", cfgPath, tt.target}, &stdout, io.Discard); got != exitViolated {
				t.Fatalf("run(explain %s) = %d, want %d\n%s", tt.target, got, exitViolated, stdout.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("出力に %q が無い:\n%s", want, stdout.String())
				}
			}
		})
	}
}

// TestExplainForm は、書式の違反が form の該当キーを、その値ごと指すことを確かめる。
func TestExplainForm(t *testing.T) {
	const cfg = `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
      - place: doc
        form:
          paragraphs: 1
disposition:
  form-paragraphs: |
    doc コメントの段落は1つです。
`
	const src = "package p\n\n// F は何かをする。\n//\n// 背景を足した段落。\nfunc F() {}\n"

	var stdout strings.Builder
	if got := run([]string{"explain", "--config", tree(t, cfg, src), "a.go:3"}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("run(explain) = %d, want %d\n%s", got, exitViolated, stdout.String())
	}
	for _, want := range []string{
		"a.go:3:1  form-paragraphs  place=doc kind=line",
		"の syntax.cstyle.allow[1].form.paragraphs です:",
		"paragraphs: 1",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("出力に %q が無い:\n%s", want, stdout.String())
		}
	}
}

// TestExplainLexicon は、層2 の違反が rules の該当エントリを指すことを確かめる。層2 の id は
// ユーザーが書いた任意の文字列で、メッセージも disposition ではなく rules[].message が持つ。
func TestExplainLexicon(t *testing.T) {
	const cfg = `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
      - place: doc
rules:
  - id: no-history
    pattern: "かつて|従来"
    message: |
      変化を語っています。今のコードが何であるかだけを書いてください。
    where:
      syntax: [cstyle]
`
	const src = "package p\n\n// F は、かつての実装を置き換えたもの。\nfunc F() {}\n"

	var stdout strings.Builder
	if got := run([]string{"explain", "--config", tree(t, cfg, src), "a.go:3"}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("run(explain) = %d, want %d\n%s", got, exitViolated, stdout.String())
	}
	for _, want := range []string{
		"a.go:3:1  no-history  place=doc kind=line",
		"変化を語っています。",
		"の rules[0] です:",
		"pattern: かつて|従来",
		"where.syntax: [cstyle]",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("出力に %q が無い:\n%s", want, stdout.String())
		}
	}
}

// TestExplainBaselined は、baseline が抑えている違反も、問われたなら説明することを確かめる
// （抑えていることは併せて告げる）。
func TestExplainBaselined(t *testing.T) {
	cfgPath := tree(t, testConfig+"baseline: .esorp-baseline.json\n", testSource)
	if got := run([]string{"baseline", "update", "--allow-new", "--config", cfgPath}, io.Discard, io.Discard); got != exitOK {
		t.Fatalf("baseline update = %d", got)
	}

	var stdout strings.Builder
	if got := run([]string{"explain", "--config", cfgPath, "a.go:6"}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("run(explain) = %d, want %d\n%s", got, exitViolated, stdout.String())
	}
	if !strings.Contains(stdout.String(), "baseline が抑えています") {
		t.Errorf("baseline が抑えていることを告げていない:\n%s", stdout.String())
	}
}

// TestExplainNoViolation は、違反の無いコメントと、コメントの無い行を書き分けることを確かめる
// （どちらも適合だが、指し損なったのかどうかは読み手に分かる必要がある）。
func TestExplainNoViolation(t *testing.T) {
	cfgPath := tree(t, testConfig, testSource)

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"適合したコメント", "a.go:4", "適合しています"},
		{"コメントの無い行", "a.go:2", "コメントはありません"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout strings.Builder
			if got := run([]string{"explain", "--config", cfgPath, tt.target}, &stdout, io.Discard); got != exitOK {
				t.Fatalf("run(explain %s) = %d, want %d\n%s", tt.target, got, exitOK, stdout.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Errorf("出力に %q が無い:\n%s", tt.want, stdout.String())
			}
		})
	}
}

// TestExplainBadTarget は、指し先の誤りが設定エラーになることを確かめる（黙って適合にしない）。
func TestExplainBadTarget(t *testing.T) {
	cfgPath := tree(t, testConfig, testSource)
	dir := filepath.Dir(cfgPath)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
	}{
		{"指し先が無い", []string{"explain", "--config", cfgPath}},
		{"指し先が2つ", []string{"explain", "--config", cfgPath, "a.go:6", "a.go:8"}},
		{"行が無い", []string{"explain", "--config", cfgPath, "a.go"}},
		{"行が数でない", []string{"explain", "--config", cfgPath, "a.go:six"}},
		{"ファイルが無い", []string{"explain", "--config", cfgPath, "無い.go:1"}},
		{"監査の対象でないファイル", []string{"explain", "--config", cfgPath, "b.txt:1"}},
		{"未知の --format", []string{"explain", "--config", cfgPath, "--format", "xml", "a.go:6"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr strings.Builder
			if got := run(tt.args, io.Discard, &stderr); got != exitConfig {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, exitConfig)
			}
			if stderr.Len() == 0 {
				t.Error("何が起きたか告げていない")
			}
		})
	}
}

// explanation は explain --format json の1件。site は違反 id と一対一なので、立っている枝で
// どこが決めたのかが分かる。
type explanation struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	ID        string `json:"id"`
	Place     string `json:"place"`
	Message   string `json:"message"`
	Baselined bool   `json:"baselined"`
	Site      struct {
		Path   string `json:"path"`
		Syntax string `json:"syntax"`
		Allow  []struct {
			Place string   `json:"place"`
			Label []string `json:"label"`
		} `json:"allow"`
		Label []string `json:"label"`
		Form  *struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		} `json:"form"`
		Rule *struct {
			ID      string `json:"id"`
			Pattern string `json:"pattern"`
			Where   struct {
				Syntax []string `json:"syntax"`
			} `json:"where"`
		} `json:"rule"`
	} `json:"site"`
}

type explainJSON struct {
	Version int    `json:"version"`
	Config  string `json:"config"`
	Target  struct {
		Path string `json:"path"`
		Line int    `json:"line"`
	} `json:"target"`
	Status       string        `json:"status"`
	Explanations []explanation `json:"explanations"`
}

// explainJSONOf は explain --format json を回し、出力を読む。
func explainJSONOf(t *testing.T, cfgPath, target string, want int) explainJSON {
	t.Helper()
	var stdout strings.Builder
	if got := run([]string{"explain", "--config", cfgPath, "--format", "json", target}, &stdout, io.Discard); got != want {
		t.Fatalf("run(explain --format json %s) = %d, want %d\n%s", target, got, want, stdout.String())
	}
	var out explainJSON
	if err := json.Unmarshal([]byte(stdout.String()), &out); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, stdout.String())
	}
	return out
}

// TestExplainJSON は、explain が text と同じ根拠（設定の場所とその中身）を機械可読で出すことを
// 確かめる。エージェントは check --format json で違反を読むので、その1件をそのまま explain に
// 渡して根拠まで JSON で引ける道が要る。
func TestExplainJSON(t *testing.T) {
	cfgPath := tree(t, testConfig, testSource)

	out := explainJSONOf(t, cfgPath, "a.go:6:2", exitViolated)
	if out.Version != 1 || out.Config != cfgPath || out.Status != "violated" {
		t.Errorf("頭が違う: %+v", out)
	}
	if out.Target.Path != "a.go" || out.Target.Line != 6 {
		t.Errorf("target が違う: %+v", out.Target)
	}
	if len(out.Explanations) != 1 {
		t.Fatalf("説明が %d 件（1 件のはず）: %+v", len(out.Explanations), out.Explanations)
	}

	e := out.Explanations[0]
	if e.Path != "a.go" || e.Line != 6 || e.ID != "place-not-allowed" || e.Place != "leading" || e.Baselined {
		t.Errorf("違反が違う: %+v", e)
	}
	if e.Message != "この位置のコメントは許可されていません。" {
		t.Errorf("message が違う: %q", e.Message)
	}
	if e.Site.Path != "syntax.cstyle.allow" || e.Site.Syntax != "cstyle" {
		t.Errorf("site が違う: %+v", e.Site)
	}
	if len(e.Site.Allow) != 3 {
		t.Fatalf("許可されている器が %d 件（3 件のはず）: %+v", len(e.Site.Allow), e.Site.Allow)
	}
	if a := e.Site.Allow[2]; a.Place != "trailing" || len(a.Label) != 1 || a.Label[0] != "TODO:" {
		t.Errorf("allow[2] が違う: %+v", a)
	}

	label := explainJSONOf(t, cfgPath, "a.go:8", exitViolated).Explanations[0]
	if s := label.Site; s.Path != "syntax.cstyle.allow[2].label" || len(s.Label) != 1 || s.Label[0] != "TODO:" {
		t.Errorf("label-required の site が違う: %+v", label.Site)
	}
}

// TestExplainJSONForm は、書式の違反が form の該当キーを、その値ごと指すことを確かめる（数は数のまま）。
func TestExplainJSONForm(t *testing.T) {
	const cfg = `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
      - place: doc
        form:
          paragraphs: 1
disposition:
  form-paragraphs: |
    doc コメントの段落は1つです。
`
	const src = "package p\n\n// F は何かをする。\n//\n// 背景を足した段落。\nfunc F() {}\n"

	e := explainJSONOf(t, tree(t, cfg, src), "a.go:3", exitViolated).Explanations[0]
	if e.ID != "form-paragraphs" || e.Site.Path != "syntax.cstyle.allow[1].form.paragraphs" {
		t.Errorf("site が違う: %+v", e.Site)
	}
	if e.Site.Form == nil || e.Site.Form.Key != "paragraphs" || e.Site.Form.Value != float64(1) {
		t.Errorf("form が違う: %+v", e.Site.Form)
	}
}

// TestExplainJSONLexicon は、層2 の違反が rules の該当エントリを、その中身ごと指すことを確かめる。
func TestExplainJSONLexicon(t *testing.T) {
	const cfg = `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: header
      - place: doc
rules:
  - id: no-history
    pattern: "かつて|従来"
    message: |
      変化を語っています。今のコードが何であるかだけを書いてください。
    where:
      syntax: [cstyle]
`
	const src = "package p\n\n// F は、かつての実装を置き換えたもの。\nfunc F() {}\n"

	e := explainJSONOf(t, tree(t, cfg, src), "a.go:3", exitViolated).Explanations[0]
	if e.ID != "no-history" || e.Site.Path != "rules[0]" || e.Site.Syntax != "" {
		t.Errorf("site が違う: %+v", e.Site)
	}
	r := e.Site.Rule
	if r == nil || r.ID != "no-history" || r.Pattern != "かつて|従来" || len(r.Where.Syntax) != 1 {
		t.Fatalf("rule が違う: %+v", r)
	}
	if r.Where.Syntax[0] != "cstyle" {
		t.Errorf("where.syntax が違う: %+v", r.Where.Syntax)
	}
}

// TestExplainJSONStatus は、違反・適合・コメント無しを status で書き分けることを確かめる（空の
// explanations では、指し損なったのかどうかが読み手に分からない）。baseline が抑えている違反も、
// 問われたなら説明する。
func TestExplainJSONStatus(t *testing.T) {
	cfgPath := tree(t, testConfig+"baseline: .esorp-baseline.json\n", testSource)
	if got := run([]string{"baseline", "update", "--allow-new", "--config", cfgPath}, io.Discard, io.Discard); got != exitOK {
		t.Fatalf("baseline update = %d", got)
	}

	out := explainJSONOf(t, cfgPath, "a.go:6", exitViolated)
	if out.Status != "violated" || len(out.Explanations) != 1 || !out.Explanations[0].Baselined {
		t.Errorf("baseline が抑えていることを告げていない: %+v", out)
	}

	if s := explainJSONOf(t, cfgPath, "a.go:4", exitOK); s.Status != "conforming" || len(s.Explanations) != 0 {
		t.Errorf("適合したコメントの status が違う: %+v", s)
	}
	if s := explainJSONOf(t, cfgPath, "a.go:2", exitOK); s.Status != "no-comment" || len(s.Explanations) != 0 {
		t.Errorf("コメントの無い行の status が違う: %+v", s)
	}
}

// TestInitDiff は、init --diff が手元の設定と現行テンプレートの差分を出すことを押さえる。testConfig は
// doc の書式を何も課しておらず、trailing のラベルもテンプレートと違うので、そこが差として出る。差分が
// あっても違反ではないので 0 で終わり、設定は書き換えない。
func TestInitDiff(t *testing.T) {
	cfgPath := tree(t, testConfig, "")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if got := run([]string{"init", "--config", cfgPath, "--diff"}, &out, io.Discard); got != exitOK {
		t.Fatalf("init --diff = %d, want %d\n%s", got, exitOK, out.String())
	}

	for _, want := range []string{
		"allow[doc].form.subject",
		"allow[trailing].label",
		"esorp は設定を書き換えません",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q が出ていない:\n%s", want, out.String())
		}
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("init --diff が設定を書き換えた")
	}
}

// TestInitDiffNoConfig は、比べる相手が無いときに設定エラーで終わることを押さえる。差分が空
// （＝テンプレートと同じ）と、設定がそもそも無いのは、別の状態。
func TestInitDiffNoConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "esorp.yaml")

	var errOut strings.Builder
	if got := run([]string{"init", "--config", missing, "--diff"}, io.Discard, &errOut); got != exitConfig {
		t.Fatalf("init --diff（設定なし） = %d, want %d", got, exitConfig)
	}
}

// reviewConfig は、層3 の口を開けた設定。question を書いてあるので review が出る。
const reviewConfig = testConfig + `
review:
  question: |
    このコメントは、目の前のコードの説明ですか。それとも、事情・履歴・作業メモですか。
`

// TestCheckReview は、層3 の材料——層1・層2 を通り抜けたコメントと、それらに投げる問い——が
// check --diff --format json に出ることを確かめる。esorp は意味を判定しないので、ここに答えは無い
// （答えるのは、この出力を読んでいるエージェント自身）。渡すのは通り抜けた doc だけで、層1 で落ちた
// leading は violations の側にいる。違反も終了コードも、層3 を開いたからといって変わらない。
func TestCheckReview(t *testing.T) {
	cfgPath := gitTree(t, reviewConfig, "// ファイル冒頭。\npackage p\n")
	dir := filepath.Dir(cfgPath)

	src := `// ファイル冒頭。
package p

// F は何かをする。
func F() {
	// 文の直前。
	_ = 1
}
`
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	if got := run([]string{"check", "--config", cfgPath, "--format", "json", "--diff", "HEAD"}, &stdout, io.Discard); got != exitViolated {
		t.Fatalf("check --diff --format json = %d, want %d\n%s", got, exitViolated, stdout.String())
	}

	var got struct {
		Version    int `json:"version"`
		Violations []struct {
			ID string `json:"id"`
		} `json:"violations"`
		Review *struct {
			Question string `json:"question"`
			Comments []struct {
				Path  string `json:"path"`
				Line  int    `json:"line"`
				Place string `json:"place"`
				Text  string `json:"text"`
			} `json:"comments"`
		} `json:"review"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, stdout.String())
	}

	if got.Version != 2 {
		t.Errorf("version = %d, want 2（review を足した形）", got.Version)
	}
	if got.Review == nil {
		t.Fatalf("review が出ていない:\n%s", stdout.String())
	}
	if !strings.Contains(got.Review.Question, "目の前のコードの説明ですか") {
		t.Errorf("問いが添えられていない: %q", got.Review.Question)
	}

	if len(got.Review.Comments) != 1 {
		t.Fatalf("渡されたコメント = %d 件, want 1: %+v", len(got.Review.Comments), got.Review.Comments)
	}
	if c := got.Review.Comments[0]; c.Place != "doc" || c.Line != 4 || !strings.Contains(c.Text, "F は何かをする") {
		t.Errorf("渡されたコメントが違う: %+v", c)
	}
	if len(got.Violations) != 1 || got.Violations[0].ID != "place-not-allowed" {
		t.Errorf("違反が変わっている: %+v", got.Violations)
	}
}

// TestCheckReviewClosedByDefault は、層3 が既定では開かないことを確かめる。設定に review: を
// 書かなければ何も出ない（ツールは既定を持たない）。書いてあっても、変更分に絞っていなければ
// 出ない（ツリー全体の通り抜けたコメントを、毎回エージェントに渡さない）。
func TestCheckReviewClosedByDefault(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  string
		args []string
	}{
		{name: "review: を書いていない", cfg: testConfig, args: []string{"--format", "json", "--diff", "HEAD"}},
		{name: "変更分に絞っていない", cfg: reviewConfig, args: []string{"--format", "json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := gitTree(t, tt.cfg, testSource)

			var stdout strings.Builder
			args := append([]string{"check", "--config", cfgPath}, tt.args...)
			run(args, &stdout, io.Discard)

			if strings.Contains(stdout.String(), `"review"`) {
				t.Errorf("層3 の口が開いている:\n%s", stdout.String())
			}
		})
	}
}

// TestReview は、esorp review が、ツリー全体の「層1・層2 を通り抜けたコメント」を問いごと渡す
// ことを確かめる。check --diff が「今書いたもの」を渡すのに対し、こちらは既にあるツリーを渡す
// （導入初日の一括レビュー）。判定しないので、違反があっても終了コードは 0 のまま。testSource で
// 層1 を通るのは header と doc の2つで、leading / orphan / ラベル無しの trailing は違反なので
// 層3 には回らない。
func TestReview(t *testing.T) {
	cfgPath := tree(t, reviewConfig, testSource)

	var stdout strings.Builder
	if got := run([]string{"review", "--config", cfgPath}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("review = %d, want %d（層3 は CI に関与しない）\n%s", got, exitOK, stdout.String())
	}

	var got struct {
		Question string `json:"question"`
		Summary  struct {
			Comments int `json:"comments"`
		} `json:"summary"`
		Comments []struct {
			Place string `json:"place"`
			Text  string `json:"text"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, stdout.String())
	}

	if !strings.Contains(got.Question, "目の前のコードの説明ですか") {
		t.Errorf("問いが添えられていない: %q", got.Question)
	}
	if got.Summary.Comments != 2 || len(got.Comments) != 2 {
		t.Fatalf("渡されたコメント = %d 件（summary %d）, want 2: %+v", len(got.Comments), got.Summary.Comments, got.Comments)
	}
	for _, c := range got.Comments {
		if c.Place != "header" && c.Place != "doc" {
			t.Errorf("違反したコメントが層3 に回っている: %+v", c)
		}
	}
}

// TestReviewPathFilter は、パスを与えると、そこに入るコメントだけが渡ることを確かめる。ツリー全体を
// 無制限に吐くと、読む側が破綻する。
func TestReviewPathFilter(t *testing.T) {
	cfgPath := tree(t, reviewConfig, testSource)
	dir := filepath.Dir(cfgPath)

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("// B は何かをする。\npackage b\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	if got := run([]string{"review", "--config", cfgPath, "sub"}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("review sub = %d, want %d\n%s", got, exitOK, stdout.String())
	}

	var got struct {
		Comments []struct {
			Path string `json:"path"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, stdout.String())
	}
	if len(got.Comments) != 1 || got.Comments[0].Path != "sub/b.go" {
		t.Fatalf("パスで絞れていない: %+v", got.Comments)
	}
}

// TestReviewClosedWithoutQuestion は、設定に review: が無ければ層3 の口が開かないことを確かめる
// （ツールは既定の問いを持たない）。開いていない口を、空の材料で開いているように見せない。
func TestReviewClosedWithoutQuestion(t *testing.T) {
	cfgPath := tree(t, testConfig, testSource)

	var stdout, stderr strings.Builder
	if got := run([]string{"review", "--config", cfgPath}, &stdout, &stderr); got != exitConfig {
		t.Fatalf("review = %d, want %d（設定に review: が無い）\n%s", got, exitConfig, stdout.String())
	}
	if !strings.Contains(stderr.String(), "review:") {
		t.Errorf("何が足りないのかを言っていない: %q", stderr.String())
	}
}

// TestInitDiffJSON は、差分が機械可読で出ること——どのキーが手元とテンプレートで何と何か、まで
// 引けることを押さえる（差分を読むのは人とはかぎらない）。引くのは、テンプレートだけが持つ項目・
// 両方にあって値が違う項目・テンプレートにしか無いエントリの3つ。
func TestInitDiffJSON(t *testing.T) {
	cfgPath := tree(t, testConfig, "")

	var out strings.Builder
	if got := run([]string{"init", "--config", cfgPath, "--diff", "--format", "json"}, &out, io.Discard); got != exitOK {
		t.Fatalf("init --diff --format json = %d, want %d\n%s", got, exitOK, out.String())
	}

	var got struct {
		Version  int    `json:"version"`
		Config   string `json:"config"`
		Same     bool   `json:"same"`
		Sections []struct {
			Title   string `json:"title"`
			Changes []struct {
				Key   string `json:"key"`
				Local string `json:"local"`
				Tmpl  string `json:"template"`
				Only  string `json:"only"`
				Text  string `json:"text"`
			} `json:"changes"`
		} `json:"sections"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("JSON が読めない: %v\n%s", err, out.String())
	}

	if got.Version != 1 || got.Config != cfgPath {
		t.Errorf("version = %d, config = %q", got.Version, got.Config)
	}
	if got.Same || len(got.Sections) == 0 {
		t.Fatalf("same = %v, sections = %d（差分があるはず）", got.Same, len(got.Sections))
	}

	type change struct{ local, tmpl, only string }
	changes := map[string]change{}
	for _, s := range got.Sections {
		for _, c := range s.Changes {
			if c.Key == "" || c.Text == "" {
				t.Errorf("キーか行が空の差分がある: %+v", c)
			}
			changes[c.Key] = change{local: c.Local, tmpl: c.Tmpl, only: c.Only}
		}
	}

	for _, want := range []struct {
		key string
		change
	}{
		{key: "syntax.cstyle.allow[doc].form.subject", change: change{tmpl: "required"}},
		{key: "syntax.cstyle.allow[trailing].label", change: change{local: "[TODO:]", tmpl: "[SAFETY: TODO: nolint:]"}},
		{key: "syntax.sgml", change: change{tmpl: "**/*.md **/*.html **/*.svg", only: "template"}},
	} {
		if got, ok := changes[want.key]; !ok || got != want.change {
			t.Errorf("%s = %+v, ある = %v; want %+v", want.key, got, ok, want.change)
		}
	}
}

// TestInitDiffJSONSame は、テンプレートと同じ設定なら same: true で、差分が空になることを押さえる。
// 「差分なし」を、機械が読める形で告げられなければ、JSON の口として用を成さない。
func TestInitDiffJSONSame(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "esorp.yaml")
	if got := run([]string{"init", "--config", cfgPath}, io.Discard, io.Discard); got != exitOK {
		t.Fatalf("init = %d, want %d", got, exitOK)
	}

	var out strings.Builder
	if got := run([]string{"init", "--config", cfgPath, "--diff", "--format", "json"}, &out, io.Discard); got != exitOK {
		t.Fatalf("init --diff --format json = %d, want %d", got, exitOK)
	}

	var got struct {
		Same     bool       `json:"same"`
		Sections []struct{} `json:"sections"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("JSON が読めない: %v\n%s", err, out.String())
	}
	if !got.Same || len(got.Sections) != 0 {
		t.Errorf("same = %v, sections = %d; want true, 0", got.Same, len(got.Sections))
	}
}

// TestInitFormatWithoutDiff は、--format が --diff の出力の形式であることを押さえる。設定の生成は
// 書くだけで出力を持たないので、黙って受け取ると、JSON が返ると思った呼び手が空を掴む。
func TestInitFormatWithoutDiff(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "esorp.yaml")

	var errOut strings.Builder
	if got := run([]string{"init", "--config", cfgPath, "--format", "json"}, io.Discard, &errOut); got != exitConfig {
		t.Fatalf("init --format json（--diff 無し） = %d, want %d", got, exitConfig)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Error("設定を書いてしまった（フラグを撥ねたなら、何もしない）")
	}
}

// lexiconSource は、層1 に反するコメント（leading）にも候補パターンが当たることを見るためのソース。
// 語彙の精度は器と関係なく決まるので、lexicon は層1 を当てない。
const lexiconSource = `package p

// F は何かをする。以前は同期だった。
func F() {
	// 以前はここで前方移行していた。
	_ = 1
}

// G は、それ以前の形式も読む。
func G() {}
`

// TestLexiconTry は、候補パターンをツリーに当てて当たりを見せることを確かめる。判定はしないので、
// 当たっても終了コードは 0（違反ではない）。
func TestLexiconTry(t *testing.T) {
	cfgPath := tree(t, testConfig, lexiconSource)

	var stdout strings.Builder
	if got := run([]string{"lexicon", "--config", cfgPath, "--try", `(^|[\s。、])以前は`}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("run(lexicon) = %d, want %d\n%s", got, exitOK, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"a.go:3:1  place=doc kind=line",
		"a.go:5:2  place=leading kind=line",
		"2 件が当たりました",
		"1 ファイル / 3 コメント中 66.67%",
		"esorp は判定しません",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "それ以前") {
		t.Errorf("直前が文頭・句読点・空白でない「以前は」に当たっている:\n%s", out)
	}
}

// TestLexiconTryJSON は、機械可読の出力に、照合に使った本文（折り返しを畳んだもの）が乗ることを
// 確かめる。原文だけでは、句が行をまたいで当たったときに、なぜ当たったのかが読み取れない。
func TestLexiconTryJSON(t *testing.T) {
	const src = "package p\n\n// F は接続を開く。\n// 以前は同期だった。\nfunc F() {}\n"
	cfgPath := tree(t, testConfig, src)

	var stdout strings.Builder
	if got := run([]string{"lexicon", "--config", cfgPath, "--format", "json", "--try", "以前は"}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("run(lexicon --format json) = %d, want %d\n%s", got, exitOK, stdout.String())
	}

	var got struct {
		Version int    `json:"version"`
		Pattern string `json:"pattern"`
		Summary struct {
			Files    int `json:"files"`
			Comments int `json:"comments"`
			Hits     int `json:"hits"`
		} `json:"summary"`
		Surfaces []struct {
			Syntax string `json:"syntax"`
			Hits   int    `json:"hits"`
		} `json:"surfaces"`
		TextSurface struct {
			Measured bool `json:"measured"`
		} `json:"text_surface"`
		Hits []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Body string `json:"body"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
		t.Fatalf("JSON が読めない: %v\n%s", err, stdout.String())
	}
	if got.Version != 2 || got.Pattern != "以前は" || got.Summary.Hits != 1 || got.Summary.Comments != 1 {
		t.Fatalf("summary が違う: %+v", got)
	}
	if len(got.Surfaces) != 1 || got.Surfaces[0].Syntax != "cstyle" || got.Surfaces[0].Hits != 1 {
		t.Errorf("面ごとの内訳が違う: %+v", got.Surfaces)
	}
	if got.TextSurface.Measured {
		t.Error("text 面には当てるコーパスが無いのに、測ったと言っている")
	}
	if len(got.Hits) != 1 || got.Hits[0].Path != "a.go" || got.Hits[0].Line != 3 {
		t.Fatalf("hits が違う: %+v", got.Hits)
	}
	if want := "F は接続を開く。以前は同期だった。"; got.Hits[0].Body != want {
		t.Errorf("body = %q, want %q（折り返しを畳んだ本文）", got.Hits[0].Body, want)
	}
}

// TestLexiconTryBadInput は、パターンを渡さない・正規表現として読めない指定を、黙って通さないことを
// 確かめる。
func TestLexiconTryBadInput(t *testing.T) {
	cfgPath := tree(t, testConfig, lexiconSource)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"--try が無い", []string{"lexicon", "--config", cfgPath}},
		{"正規表現として読めない", []string{"lexicon", "--config", cfgPath, "--try", "(?i)["}},
		{"余分な引数", []string{"lexicon", "--config", cfgPath, "--try", "以前は", "b.go"}},
		{"未知の --format", []string{"lexicon", "--config", cfgPath, "--try", "以前は", "--format", "yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := run(tc.args, io.Discard, io.Discard); got != exitConfig {
				t.Fatalf("run(%v) = %d, want %d", tc.args, got, exitConfig)
			}
		})
	}
}

// TestReviewFlagAfterPath は、<path> の後ろに置かれたフラグを、黙ってパスとして飲み込まないことを
// 確かめる。Go の flag は最初の非フラグ引数で解析を止めるので、そのままでは --format が無視され、
// 指定していない形式で出てしまう（指定が黙って効かないのが、いちばん悪い）。
func TestReviewFlagAfterPath(t *testing.T) {
	cfgPath := tree(t, reviewConfig, testSource)

	var stdout, stderr strings.Builder
	if got := run([]string{"review", "--config", cfgPath, "internal", "--format", "text"}, &stdout, &stderr); got != exitConfig {
		t.Fatalf("review <path> --format text = %d, want %d\n%s", got, exitConfig, stdout.String())
	}
	if !strings.Contains(stderr.String(), "前に置いて") {
		t.Errorf("直し方を言っていない: %q", stderr.String())
	}
}
