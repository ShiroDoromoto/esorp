package rule

import (
	"regexp"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// rules は、設定を読んだ後と同じ形のルール（Regexp が組み立ててある）を作る。
func rules(t *testing.T, rs ...config.Rule) []config.Rule {
	t.Helper()

	for i, r := range rs {
		rs[i].Regexp = regexp.MustCompile(r.Pattern)
	}
	return rs
}

// lexicon は、Go のソース断片の全コメントをルールに照らす（層1 は通ったものとして扱う）。
func lexicon(t *testing.T, src string, rs []config.Rule, target Target) []Violation {
	t.Helper()

	return lexiconWith(t, src, rs, target, scan.GoSpec())
}

// lexiconWith は、言語を指定して全コメントをルールに照らす。
func lexiconWith(t *testing.T, src string, rs []config.Rule, target Target, spec scan.LangSpec) []Violation {
	t.Helper()

	var out []Violation
	for _, c := range place.Classify(scan.CStyle([]byte(src), spec), spec) {
		out = append(out, Lexicon(c, rs, target, spec)...)
	}
	return out
}

// history は、「変化を指す専用句」を落とすルール。プロジェクトが自分で足すものであり、ツールの
// 既定ではない。
func history(t *testing.T) []config.Rule {
	return rules(t, config.Rule{
		ID:      "no-history",
		Pattern: `no longer|used to|かつて|従来`,
		Message: "変化を語っています。今のコードの説明に書き直すか、削除してください。",
	})
}

func TestLexicon(t *testing.T) {
	src := "package p\n\n// F はかつて同期だった。\nfunc F() {}\n\n// G は値を返す。\nfunc G() int { return 1 }\n"

	got := lexicon(t, src, history(t), Target{Syntax: "cstyle", Path: "a.go"})
	if len(got) != 1 {
		t.Fatalf("違反 = %d 件, want 1\n%#v", len(got), got)
	}
	if got[0].ID != "no-history" {
		t.Errorf("id = %q, want no-history", got[0].ID)
	}
	if got[0].Line != 3 {
		t.Errorf("行 = %d, want 3", got[0].Line)
	}
	if got[0].Message != history(t)[0].Message {
		t.Errorf("メッセージ = %q, want ルールの message（disposition は層1 のためのもの）", got[0].Message)
	}
}

// TestLexiconNoRules は、ツールが既定のルールを持たないことを押さえる。rules: が空なら、どんな
// 本文でも何も起きない。
func TestLexiconNoRules(t *testing.T) {
	src := "package p\n\n// F はかつて同期だった。legacy な old コード。\nfunc F() {}\n"

	if got := lexicon(t, src, nil, Target{Syntax: "cstyle", Path: "a.go"}); len(got) != 0 {
		t.Errorf("ルールが無いのに %d 件の違反が出た\n%#v", len(got), got)
	}
}

// TestLexiconWhere は、where: の3軸（syntax / kind / path）がルールの届く先を絞ることを押さえる。
// 省略した軸は絞らない。
func TestLexiconWhere(t *testing.T) {
	src := "package p\n\n// F はかつて同期だった。\nfunc F() {}\n"
	doc := Target{Syntax: "cstyle", Path: "internal/a.go"}

	tests := []struct {
		name   string
		where  config.Where
		target Target
		want   int
	}{
		{"省略時は絞らない", config.Where{}, doc, 1},
		{"syntax に当たる", config.Where{Syntax: []string{"cstyle"}}, doc, 1},
		{"syntax が違う", config.Where{Syntax: []string{"cstyle-test"}}, doc, 0},
		{"kind に当たる", config.Where{Kind: []string{"line"}}, doc, 1},
		{"kind が違う", config.Where{Kind: []string{"block"}}, doc, 0},
		{"path に当たる", config.Where{Path: []string{"internal/**"}}, doc, 1},
		{"path が違う", config.Where{Path: []string{"cmd/**"}}, doc, 0},
		{"path の除外はいつも勝つ", config.Where{Path: []string{"**/*.go", "!internal/**"}}, doc, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := history(t)
			rs[0].Where = tt.where

			if got := lexicon(t, src, rs, tt.target); len(got) != tt.want {
				t.Errorf("違反 = %d 件, want %d\n%#v", len(got), tt.want, got)
			}
		})
	}
}

// TestLexiconWrapped は、折り返しで途切れた句に当たることを見る。80桁で折り返す流儀のコメントは、
// 句が改行をまたぐ。当たらないまま通すと、検査したのに適合したように見える。
func TestLexiconWrapped(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{
			"英語の句が行をまたぐ",
			"package p\n\n// G does not do this no\n// longer.\nfunc G() {}\n",
			1,
		},
		{
			"日本語の句が行をまたぐ",
			"package p\n\n// F はかつ\n// て同期だった。\nfunc F() {}\n",
			1,
		},
		{
			"折り返していない句には今までどおり当たる",
			"package p\n\n// F はかつて同期だった。\nfunc F() {}\n",
			1,
		},
		{
			"句を含まないコメントには当たらない",
			"package p\n\n// G は値を返す。\nfunc G() int { return 1 }\n",
			0,
		},
		{
			"段落をまたいだ語のつながりには当たらない",
			"package p\n\n// F は no\n//\n// longer を含む段落を持つ。\nfunc F() {}\n",
			0,
		},
		{
			"doc のコードブロックの行をまたいだつながりには当たらない",
			"package p\n\n// F は変換する。\n//\n//\tx := no\n//\tlonger(x)\n//\nfunc F() {}\n",
			0,
		},
		{
			"コードブロックと地続きの散文にも当たらない",
			"package p\n\n// F は no\n//\n//\tlonger()\nfunc F() {}\n",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lexicon(t, tt.src, history(t), Target{Syntax: "cstyle", Path: "a.go"})
			if len(got) != tt.want {
				t.Errorf("違反 = %d 件, want %d\n%#v", len(got), tt.want, got)
			}
		})
	}
}

// TestLexiconFencedCodeBlock は、フェンスで囲んだコードブロックの行を畳まないことを見る。Rust / TS の
// doc はコード例をフェンスで書くので、畳むとコードの行どうしがつながり、ルールが行をまたいで当たる。
// 行の中では今までどおり当たる（コードブロックを層2 の対象から外しはしない）。
func TestLexiconFencedCodeBlock(t *testing.T) {
	tests := []struct {
		name string
		src  string
		spec scan.LangSpec
		want int
	}{
		{
			"Rust: フェンスの中の行をまたいだつながりには当たらない",
			"/// f は変換する。\n///\n/// ```\n/// let x = no\n/// longer(x);\n/// ```\npub fn f() {}\n",
			scan.RustSpec(),
			0,
		},
		{
			"Rust: フェンスの中でも、1行に収まった句には当たる",
			"/// f は変換する。\n///\n/// ```\n/// // f は no longer 使わない\n/// ```\npub fn f() {}\n",
			scan.RustSpec(),
			1,
		},
		{
			"TypeScript: JSDoc のフェンスの中の行をまたいだつながりにも当たらない",
			"/**\n * f は変換する。\n *\n * ```ts\n * const x = no\n * longer(x);\n * ```\n */\nexport function f() {}\n",
			scan.TSSpec(),
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lexiconWith(t, tt.src, history(t), Target{Syntax: "cstyle", Path: "a.rs"}, tt.spec)
			if len(got) != tt.want {
				t.Errorf("違反 = %d 件, want %d\n%#v", len(got), tt.want, got)
			}
		})
	}
}

// TestLexiconSite は、層2 の違反が、それを出した rules のエントリを指すことを確かめる。層2 の id は
// ユーザーが書いた任意の文字列なので、explain は id ではなく、この添字で設定の該当箇所を引く。
func TestLexiconSite(t *testing.T) {
	rs := rules(t,
		config.Rule{ID: "no-todo", Pattern: "TODO", Message: "起票してください。"},
		config.Rule{ID: "no-history", Pattern: "かつて", Message: "今のコードの説明に書き直してください。"},
	)

	got := lexicon(t, "// F は、かつての実装を置き換えたもの。\nfunc F() {}\n", rs, Target{Syntax: "cstyle", Path: "a.go"})
	if len(got) != 1 {
		t.Fatalf("違反が %d 件（1 件のはず）: %#v", len(got), got)
	}
	if s := got[0].Site; s.Path != "rules[1]" || s.Rule != 1 || s.Allow != -1 {
		t.Errorf("当たったルールを指していない: %#v", s)
	}
}
