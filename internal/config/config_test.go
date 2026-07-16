package config

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

func load(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "esorp.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// TestLoadTemplate は、esorp init が生成するテンプレートそのものを読み、subject が Go のエントリに
// だけ立つこと（Rust / TypeScript の doc 規約ではない）と、leading / orphan をどのエントリも許可
// しないこと（それがこのツールの効き目そのもの）を確かめる。生成した設定がその場で設定エラーに
// なれば、最初の一歩で信頼を失う。
func TestLoadTemplate(t *testing.T) {
	cfg, err := load(t, Template)
	if err != nil {
		t.Fatalf("テンプレートが読めない: %v", err)
	}

	for name, wantSubject := range map[string]string{
		"cstyle-go":   "required",
		"cstyle-rust": "",
		"cstyle-ts":   "",
	} {
		s, ok := cfg.Syntax[name]
		if !ok {
			t.Fatalf("syntax.%s がない", name)
		}
		if s.Family != "cstyle" || s.Mode != "structural" || len(s.Allow) != 3 {
			t.Fatalf("syntax.%s = %#v", name, s)
		}
		if got := s.Allow[1].PlaceValue(); got != place.Doc {
			t.Errorf("syntax.%s.allow[1].place = %v, want doc", name, got)
		}
		f := s.Allow[1].Form
		if f == nil || f.Paragraphs == nil || *f.Paragraphs != 1 || f.Headings != "deny" {
			t.Fatalf("syntax.%s.allow[1].form = %#v", name, f)
		}
		if f.Subject != wantSubject {
			t.Errorf("syntax.%s.allow[1].form.subject = %q, want %q", name, f.Subject, wantSubject)
		}
		if len(s.Allow[2].Label) == 0 {
			t.Errorf("syntax.%s.allow[2].label が空（行末はラベル必須）", name)
		}
	}

	for name, s := range cfg.Syntax {
		for _, a := range s.Allow {
			if p := a.PlaceValue(); p == place.Leading || p == place.Orphan {
				t.Errorf("syntax.%s が %v を許可している", name, p)
			}
		}
	}

	if !cfg.RespectGitignore {
		t.Error("respect_gitignore = false, want true")
	}
	rules := map[string]Rule{}
	for _, r := range cfg.Rules {
		rules[r.ID] = r
	}
	if len(cfg.Rules) != 2 || len(rules) != 2 {
		t.Fatalf("rules = %v, want プリセット2件（init は現物を書き込んで吐く）", cfg.Rules)
	}
	for _, id := range []string{"no-history", "internal-ref"} {
		if r, ok := rules[id]; !ok {
			t.Errorf("プリセットに %q が無い", id)
		} else if r.Message == "" {
			t.Errorf("rules[%s].message が空（違反の始末のしかたを言えない）", id)
		}
	}

	h := rules["no-history"]
	if len(h.Where.Syntax) != 0 || len(h.Where.Kind) != 0 || len(h.Where.Path) != 0 {
		t.Errorf("rules[no-history].where = %+v, want 省略（全エントリに当てる）", h.Where)
	}
	for _, s := range []string{"this used to", "かつて", "従来は"} {
		if !h.Regexp.MatchString(s) {
			t.Errorf("プリセットが %q に当たらない", s)
		}
	}
	for _, s := range []string{"no longer needed", "is used to build the index", "従来どおり"} {
		if h.Regexp.MatchString(s) {
			t.Errorf("プリセットが %q に当たる（実測で偽陽性が支配的だった形）", s)
		}
	}

	ref := rules["internal-ref"]
	if !ref.Regexp.MatchString("closes #123") {
		t.Error("internal-ref が追跡番号に当たらない")
	}
	if ref.Where.SelectsSyntax(SyntaxText) {
		t.Errorf("internal-ref.where.syntax = %v, want text 抜き（参照の正しい行き先を塞がない）", ref.Where.Syntax)
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Syntax)) {
		if !ref.Where.SelectsSyntax(name) {
			t.Errorf("internal-ref.where.syntax = %v, want %q を選ぶ（列挙すると、案内どおりに syntax: を削った設定が exit 2 で壊れる）", ref.Where.Syntax, name)
		}
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

// TestLoadDeclaredComments は、コメント記法を宣言したエントリが読めることを見る。宣言があれば
// キーはファミリ名でなくてよく（nsis）、family: も lang: も要らない——読み方は comments: が決める。
// 複数の行コメント記号と、ブロックの対を受ける。
func TestLoadDeclaredComments(t *testing.T) {
	cfg, err := load(t, `
syntax:
  nsis:
    files: ["**/*.nsh"]
    mode: content-only
    comments:
      line: [";", "#"]
      block: [["/*", "*/"]]
`)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	c := cfg.Syntax["nsis"].Comments
	if !c.Declared() {
		t.Fatal("Declared() が false")
	}
	if !slices.Equal(c.Line, []string{";", "#"}) {
		t.Errorf("line = %v", c.Line)
	}
	if pairs := c.BlockPairs(); len(pairs) != 1 || pairs[0] != [2]string{"/*", "*/"} {
		t.Errorf("BlockPairs = %v", pairs)
	}
}

// TestLoadRuleWhereSyntaxText は、where.syntax の予約値 text が、syntax: に同名のエントリが
// 無くても読めることを見る。取り出しの無い入力は拾う対象を持たないので、宣言できない。
func TestLoadRuleWhereSyntaxText(t *testing.T) {
	cfg, err := load(t, `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: content-only
rules:
  - id: no-history
    pattern: "かつて"
    message: "変化を語っています"
    where:
      syntax: [cstyle, text]
`)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	if got := cfg.Rules[0].Where.Syntax; !slices.Contains(got, SyntaxText) {
		t.Errorf("where.syntax = %v, want %q を含む", got, SyntaxText)
	}
}

// TestLoadRuleWhereSyntaxExcludeOnly は、除外だけの並びが通り、「それ以外すべて」を選ぶことを見る。
// where.path は正の glob が0本だとエラーだが、syntax の値域は syntax: のキーと予約値 text で閉じて
// いるので、「これ以外」が曖昧さなく決まる。ここを path に揃えて塞ぐと、text を外す唯一の手が
// 消える（init テンプレートの internal-ref がそれを使う）。
func TestLoadRuleWhereSyntaxExcludeOnly(t *testing.T) {
	cfg, err := load(t, `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: content-only
rules:
  - id: internal-ref
    pattern: '#\d+'
    message: "追跡番号への参照です"
    where:
      syntax: ["!text"]
`)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	w := cfg.Rules[0].Where
	if w.SelectsSyntax(SyntaxText) {
		t.Errorf("SelectsSyntax(%q) = true, want false", SyntaxText)
	}
	if !w.SelectsSyntax("cstyle") {
		t.Error("SelectsSyntax(\"cstyle\") = false, want true（除外だけの並びは、それ以外すべてを選ぶ）")
	}
}

// TestLoadSeverity は、severity: が層1 の違反 id と rules[].id を分け隔てなく1枚で引けること
// （鍵の空間は1つ）と、書かれていない id が読まれないままでいることを見る。
func TestLoadSeverity(t *testing.T) {
	cfg, err := load(t, `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
rules:
  - id: no-history
    pattern: "かつて"
    message: "変化を語っています"
severity:
  form-paragraphs: advisory
  no-history: advisory
  place-not-allowed: enforce
`)
	if err != nil {
		t.Fatalf("読めない: %v", err)
	}
	want := map[string]string{
		"form-paragraphs":   SeverityAdvisory,
		"no-history":        SeverityAdvisory,
		"place-not-allowed": SeverityEnforce,
	}
	if !maps.Equal(cfg.Severity, want) {
		t.Errorf("severity = %v, want %v", cfg.Severity, want)
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
			want: "not a known place class",
		},
		{
			name: "未知の mode",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: strict\n",
			want: "mode",
		},
		{
			name: "字句の無いファミリ",
			body: "syntax:\n  lisp:\n    files: [\"**/*.el\"]\n    mode: content-only\n",
			want: "there is no scanner that reads",
		},
		{
			name: "無い字句を名指しする lang",
			body: "syntax:\n  cstyle:\n    lang: golang\n    files: [\"**/*.go\"]\n    mode: content-only\n",
			want: "there is no lexer named",
		},
		{
			name: "ファミリと食い違う lang",
			body: "syntax:\n  hash:\n    lang: go\n    files: [\"**/*.sh\"]\n    mode: content-only\n",
			want: "contradicts family:",
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
			name: "不正な glob は、黙って何にも当たらないままにしない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\", \"src/[.go\"]\n    mode: structural\n",
			want: "is not a valid glob",
		},
		{
			name: "除外だけでは何も拾わない",
			body: "syntax:\n  cstyle:\n    files: [\"!vendor/**\"]\n    mode: structural\n",
			want: "exclusions alone",
		},
		{
			name: "subject は doc のときだけ",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: trailing\n        form:\n          subject: required\n",
			want: "can only be given under place: doc",
		},
		{
			name: "正規表現が不正",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: bad\n    pattern: \"(\"\n    message: x\n",
			want: "invalid regular expression",
		},
		{
			name: "rules の id が重複",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: dup\n    pattern: a\n    message: x\n  - id: dup\n    pattern: b\n    message: y\n",
			want: "is a duplicate",
		},
		{
			name: "where.syntax が syntax に無い名前",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      syntax: [hash]\n",
			want: "is not a name in syntax:",
		},
		{
			name: "where.syntax の除外も syntax に在る名前でなければならない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      syntax: [\"!hash\"]\n",
			want: "is not a name in syntax:",
		},
		{
			name: "where.kind の「!」は、綴りではなく除外の口が無いことを言う",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      kind: [\"!line\"]\n",
			want: "kind has no exclusion",
		},
		{
			name: "syntax に text エントリは書けない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n  text:\n    files: [\"**/*.txt\"]\n    mode: content-only\n",
			want: "reserved name",
		},
		{
			name: "rules の id が層1 の違反 id と衝突",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: form-headings\n    pattern: a\n    message: x\n",
			want: "is a layer 1 violation id",
		},
		{
			name: "where.path の glob が不正",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      path: [\"src/[.go\"]\n",
			want: "is not a valid glob",
		},
		{
			name: "where.path が除外だけでは何にも当たらない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      path: [\"!vendor/**\"]\n",
			want: "exclusions alone",
		},
		{
			name: "where.syntax の同じ名前が正と除外に並ぶ",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      syntax: [cstyle, \"!cstyle\"]\n",
			want: "cancel out",
		},
		{
			name: "打ち消しは、生き残る名前が他に在っても許さない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n  hash:\n    files: [\"**/*.yml\"]\n    mode: content-only\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      syntax: [cstyle, \"!cstyle\", hash]\n",
			want: "cancel out",
		},
		{
			name: "where.syntax の予約値 text も打ち消せば同じ",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      syntax: [text, \"!text\"]\n",
			want: "cancel out",
		},
		{
			name: "where.path の同じ glob が正と除外に並ぶ",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nrules:\n  - id: r\n    pattern: a\n    message: x\n    where:\n      path: [\"**/*.go\", \"!**/*.go\"]\n",
			want: "cancel out",
		},
		{
			name: "syntax.files の同じ glob が正と除外に並ぶ",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\", \"!**/*.go\"]\n    mode: structural\n",
			want: "cancel out",
		},
		{
			name: "disposition の違反 id が不明",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\ndisposition:\n  place-not-allowd: x\n",
			want: "not a known violation id",
		},
		{
			name: "severity の違反 id が不明",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nseverity:\n  place-not-allowd: advisory\n",
			want: "not a known violation id",
		},
		{
			name: "severity の値が enforce / advisory ではない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nseverity:\n  form-refs: warn\n",
			want: "is not a known strength",
		},
		{
			name: "severity は off を持たない",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\nseverity:\n  form-refs: off\n",
			want: "is not a known strength",
		},
		{
			name: "syntax が空",
			body: "rules: []\n",
			want: "syntax",
		},
		{
			name: "段落数の上限が 0",
			body: "syntax:\n  cstyle:\n    files: [\"**/*.go\"]\n    mode: structural\n    allow:\n      - place: doc\n        form:\n          paragraphs: 0\n",
			want: "cap on the number of paragraphs",
		},
		{
			name: "comments は structural には書けない",
			body: "syntax:\n  nsis:\n    files: [\"**/*.nsh\"]\n    mode: structural\n    comments:\n      line: [\";\"]\n",
			want: "can only be declared under mode: content-only",
		},
		{
			name: "comments と lang は併記できない",
			body: "syntax:\n  nsis:\n    lang: shell\n    files: [\"**/*.nsh\"]\n    mode: content-only\n    comments:\n      line: [\";\"]\n",
			want: "cannot be written alongside lang:",
		},
		{
			name: "comments と family は併記できない",
			body: "syntax:\n  nsis:\n    family: hash\n    files: [\"**/*.nsh\"]\n    mode: content-only\n    comments:\n      line: [\";\"]\n",
			want: "cannot be written alongside family:",
		},
		{
			name: "block の対は開きと閉じの2つ",
			body: "syntax:\n  nsis:\n    files: [\"**/*.nsh\"]\n    mode: content-only\n    comments:\n      block: [[\"/*\"]]\n",
			want: "the opener and the closer",
		},
		{
			name: "block の開き・閉じは空にできない",
			body: "syntax:\n  nsis:\n    files: [\"**/*.nsh\"]\n    mode: content-only\n    comments:\n      block: [[\"/*\", \"\"]]\n",
			want: "neither the opener nor the closer can be empty",
		},
		{
			name: "line の記号は空にできない",
			body: "syntax:\n  nsis:\n    files: [\"**/*.nsh\"]\n    mode: content-only\n    comments:\n      line: [\"\"]\n",
			want: "an empty marker cannot be written",
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

// TestReviewNeedsQuestion は、問いの無い review: を設定エラーにすることを確かめる。層3 は
// 「通り抜けたコメントを渡し、問いを添える」ことが全部なので、問いが無ければ渡す意味が無い。
// 黙って空の問いを渡すより、設定を書いた時点で気づける方がよい。
func TestReviewNeedsQuestion(t *testing.T) {
	_, err := load(t, `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: doc
review: {}
`)
	if err == nil {
		t.Fatal("問いの無い review: が通った")
	}
	if !strings.Contains(err.Error(), "review.question") {
		t.Errorf("何が悪いのかを言っていない: %v", err)
	}
}

// TestReviewAbsentIsFine は、review: を書かなければ層3 が開かないだけで、設定として正しいことを
// 確かめる。ツールは層3 の既定を持たない。
func TestReviewAbsentIsFine(t *testing.T) {
	cfg, err := load(t, `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: structural
    allow:
      - place: doc
`)
	if err != nil {
		t.Fatalf("review: の無い設定が通らない: %v", err)
	}
	if cfg.Review != nil {
		t.Error("書いていない review: が入っている")
	}
}
