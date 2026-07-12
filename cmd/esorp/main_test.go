package main

import (
	"encoding/json"
	"io"
	"os"
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

// 器が1つずつ現れる: header / doc（適合）と leading / orphan / ラベル無しの trailing（違反）。
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

// 字句を持たない言語のファイルは、検査していないことを告げる（黙って適合にしない）。
func TestCheckWarnsOnUnscannableFiles(t *testing.T) {
	cfgPath := tree(t, "syntax:\n  cstyle:\n    files: [\"**/*.rs\"]\n    mode: structural\n", "")
	if err := os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "a.rs"), []byte("// x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if got := run([]string{"check", "--config", cfgPath}, &stdout, &stderr); got != exitOK {
		t.Fatalf("run(check) = %d, want %d", got, exitOK)
	}
	if !strings.Contains(stderr.String(), "a.rs") {
		t.Errorf("読めなかったファイルを告げていない: %q", stderr.String())
	}
}
