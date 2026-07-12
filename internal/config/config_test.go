package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// template は、esorp init が生成する設定テンプレートと同じ形。これが読めることを確かめる。
const template = `
syntax:
  cstyle:
    files:
      - "**/*.go"
      - "**/*.rs"
    mode: structural
    allow:
      - place: header

      - place: doc
        form:
          subject: required
          headings: deny
          paragraphs: 1
          refs: deny

      - place: trailing
        label: ["SAFETY:", "TODO:", "nolint:"]

disposition:
  place-not-allowed: |
    この位置のコメントは許可されていません。

respect_gitignore: true

rules: []

baseline: .esorp-baseline.json
`

func load(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "esorp.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestLoadTemplate(t *testing.T) {
	cfg, err := load(t, template)
	if err != nil {
		t.Fatalf("テンプレートが読めない: %v", err)
	}

	s := cfg.Syntax["cstyle"]
	if s.Mode != "structural" || len(s.Files) != 2 || len(s.Allow) != 3 {
		t.Fatalf("syntax.cstyle = %#v", s)
	}
	if got := s.Allow[1].PlaceValue(); got != place.Doc {
		t.Errorf("allow[1].place = %v, want doc", got)
	}
	if f := s.Allow[1].Form; f == nil || f.Subject != "required" || f.Paragraphs == nil || *f.Paragraphs != 1 {
		t.Errorf("allow[1].form = %#v", f)
	}
	if got := s.Allow[2].Label; len(got) != 3 || got[0] != "SAFETY:" {
		t.Errorf("allow[2].label = %v", got)
	}
	if cfg.Baseline != ".esorp-baseline.json" {
		t.Errorf("baseline = %q", cfg.Baseline)
	}
	if !cfg.RespectGitignore {
		t.Error("respect_gitignore = false, want true")
	}
	if len(cfg.Rules) != 0 {
		t.Errorf("rules = %v, want 空", cfg.Rules)
	}
}

func TestLoadRuleCompilesPattern(t *testing.T) {
	cfg, err := load(t, `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: doc
rules:
  - id: history-ja
    pattern: "(かつて|従来)"
    message: "変更の履歴です。削除してください"
    where:
      syntax: [cstyle]
      kind: [line, block]
`)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	r := cfg.Rules[0]
	if r.Regexp == nil || !r.Regexp.MatchString("かつてはここで移行していた") {
		t.Errorf("rules[0].Regexp が組み立てられていない: %#v", r.Regexp)
	}
	if got := cfg.Syntax["cstyle"].Allow[0].KindValues(); got != nil {
		t.Errorf("kind 省略時は全 kind なので空: %v", got)
	}
	if _, ok := scan.ParseKind("line"); !ok {
		t.Error("scan.ParseKind(line) が引けない")
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		body string

		// want はエラーに現れるべき断片。
		want string
	}{
		{
			name: "未知のキーは黙って無視せず拒否する",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    alow:\n      - place: doc\n",
			want: "alow",
		},
		{
			name: "未知の位置クラス",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: docs\n",
			want: "不明な位置クラス",
		},
		{
			name: "未知の mode",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: strict\n",
			want: "mode",
		},
		{
			name: "スキャナの無いファミリ",
			body: "syntax:\n  hash:\n    files: [\"**/*.sh\"]\n    mode: content-only\n",
			want: "スキャナがありません",
		},
		{
			name: "content-only に allow は書けない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: content-only\n    allow:\n      - place: doc\n",
			want: "content-only",
		},
		{
			name: "files が無い",
			body: "syntax:\n  cstyle:\n    mode: structural\n",
			want: "files",
		},
		{
			name: "subject は doc のときだけ",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: trailing\n        form:\n          subject: required\n",
			want: "place: doc のときだけ",
		},
		{
			name: "正規表現が不正",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: bad\n    pattern: \"(\"\n    message: x\n",
			want: "正規表現が不正",
		},
		{
			name: "rules の id が重複",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: dup\n    pattern: a\n    message: x\n  - id: dup\n    pattern: b\n    message: y\n",
			want: "重複",
		},
		{
			name: "where.syntax が syntax に無い名前",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      syntax: [hash]\n",
			want: "syntax: に無い名前",
		},
		{
			name: "disposition の違反 id が不明",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\ndisposition:\n  place-not-allowd: x\n",
			want: "不明な違反 id",
		},
		{
			name: "syntax が空",
			body: "rules: []\n",
			want: "syntax",
		},
		{
			name: "段落数の上限が 0",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: doc\n        form:\n          paragraphs: 0\n",
			want: "段落数の上限",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(t, tt.body)
			if err == nil {
				t.Fatal("設定エラーになっていない")
			}
			var cerr *Error
			if !errors.As(err, &cerr) {
				t.Fatalf("*config.Error でない: %T", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("エラーに %q が現れない:\n%v", tt.want, err)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "no-such-file.yaml"))
	var cerr *Error
	if err == nil || !errors.As(err, &cerr) {
		t.Fatalf("設定が読めないことは設定エラー: %v", err)
	}
}

// TestLoadReportsAllProblems は、検証が1つ目で打ち切らずすべて挙げることを確かめる（設定を1回で
// 直せるように）。
func TestLoadReportsAllProblems(t *testing.T) {
	_, err := load(t, "syntax:\n  cstyle:\n    mode: strict\n    allow:\n      - place: docs\n")
	var cerr *Error
	if !errors.As(err, &cerr) {
		t.Fatalf("*config.Error でない: %v", err)
	}
	if len(cerr.Problems) < 2 {
		t.Errorf("問題が %d 件しか挙がっていない: %v", len(cerr.Problems), cerr.Problems)
	}
}
