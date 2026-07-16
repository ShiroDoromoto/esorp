// Package config は、esorp.yaml の読み込みとスキーマ検証を担う。
//
// 設定ファイルが唯一の真実であり、ツールは実行時の既定を持たない。書かれていないものは
// 効かない。既定は esorp init が生成するテンプレートとしてだけ存在する。
// その帰結として、綴り違いのキーを黙って無視することは許されない（見えているものが動いている
// ものの全部でなくなる）。未知のキーは設定エラーにする。
package config

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/ShiroDoromoto/esorp/internal/glob"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// knownFamilies は、字句を持っている構文ファミリ。ここに無いファミリは設定に書けない
// （書けてしまうと、黙って何も検査しない）。
var knownFamilies = []string{"cstyle", "hash", "sgml", "cssblock"}

// SyntaxText は、取り出しの無い入力（ファイルから拾ったのではない素の本文）を指す where.syntax の
// 予約値。面を絞る軸は where.syntax のままで、text はその一員。syntax: セクションには書けない
// ——files を持たない入力なので、拾う対象が無い。
const SyntaxText = "text"

// knownViolations は、層1 が出す違反 id。disposition のキーはこれでなければならない。
var knownViolations = []string{
	"place-not-allowed", "label-required",
	"form-subject", "form-headings", "form-paragraphs", "form-max-lines", "form-urls",
}

// severity の値。書かれていない id は Enforce で、ツールの中に隠れた強度は無い。
// 「効かせない」を意味する3つ目の値は持たない——層1 の書式は headings: allow / subject: off、
// 器は allow に列挙する、層2 は rules: からエントリを消す、と既存の言い方で全部言えるので、
// 二つ目の言い方を足せば同じことが二箇所で言えて必ずドリフトする。
const (
	SeverityEnforce  = "enforce"
	SeverityAdvisory = "advisory"
)

var knownSeverities = []string{SeverityEnforce, SeverityAdvisory}

// Config は esorp.yaml の全体。
type Config struct {
	Syntax      map[string]Syntax `yaml:"syntax"`
	Disposition map[string]string `yaml:"disposition"`

	// Severity は違反 id ごとの強制の強度（id → enforce | advisory）。鍵は層1 の違反 id と
	// rules[].id を合わせた1つの id 空間で、層1・層2 を分け隔てなく1枚で扱える——rules[].id が
	// 層1 の id と衝突しないことは、設定検証が強制している。
	Severity map[string]string `yaml:"severity"`

	Rules  []Rule  `yaml:"rules"`
	Review *Review `yaml:"review"`

	// RespectGitignore は、gitignore されたものを走査から外すか。git が「自分のコードではない」と
	// 宣言しているものを、esorp も自分のコードとして扱わない。gitignore を黙って見にいくのは設定に
	// 見えない挙動になるので、方針としてここに書かせる。git リポジトリでなければ効かない。
	RespectGitignore bool `yaml:"respect_gitignore"`
}

// Review は層3（意味）の口。esorp はコメントの意味を判定しない。層1（器と書式）と層2（語彙）を
// 通り抜けたコメントを、変更分に絞って機械可読で渡し、この問いを添えるだけで、答えるのは esorp を
// 走らせているエージェント自身。書かなければ層3 は開かない（既定を持たない）。
type Review struct {
	// Question は、通り抜けたコメント1つずつに対してエージェントへ投げる問い。二値で答えられる
	// ものにする。カテゴリに分けさせると、決定論で解けなかった分類問題を確率的な機械に押し付ける
	// ことになり、非決定性が暴れる。
	Question string `yaml:"question"`
}

// Syntax は構文ファミリごとのエントリ。キーが cstyle のようなファミリ名そのものであれば
// Family は省略でき、本体は違う設定で使い分けるとき（cstyle-src / cstyle-test）に指す。
type Syntax struct {
	Family string `yaml:"family"`

	// Lang は、このエントリのファイルを読む字句を名指しする（lang: go）。省略すると、字句は
	// ファイルの名前・拡張子から引き、それも空振りならファミリの既定になる。名指しが要るのは、
	// 拡張子も既知の名前も持たない C 系のファイル（生成物・フック）で、cstyle はファミリの既定を
	// 持たない（器の判定が言語ごとの宣言の語彙に依るため）。
	Lang string `yaml:"lang"`

	// Files は、このファミリのスキャナで読むファイルの glob。拡張子ではなく glob なのは、
	// 拡張子を持たないファイル（Makefile）があるため。
	Files []string `yaml:"files"`

	// Mode は検査の深さ（structural | content-only）。
	Mode string `yaml:"mode"`

	// Allow は許可する器の列挙。mode: structural のときだけ書ける。
	Allow []Allow `yaml:"allow"`

	// Comments は、このエントリのファイルを読むコメント記法の宣言（mode: content-only 限定）。
	// プリセットの字句（cstyle / hash / sgml / cssblock）で読めない拡張子に網を張るための逃げ道で、
	// 宣言があれば名前・拡張子・ファミリからの字句解決より優先する（設定が唯一の真実）。
	// lang: / family: とは併記できない——どちらも読み方の宣言で、食い違う。
	Comments Comments `yaml:"comments"`
}

// Comments は、エントリが宣言するコメント記法。層1（器）の宣言の解析は要らない content-only に
// 限るので、行コメントとブロックコメントの記号だけを持つ。
type Comments struct {
	// Line は行コメントを開く記号の並び（";" "#" など）。
	Line []string `yaml:"line"`

	// Block はブロックコメントの開き/閉じの対の並び（[["/*", "*/"]]）。
	Block [][]string `yaml:"block"`
}

// Declared は、コメント記法が宣言されているか（行かブロックのどちらかが書かれている）。
func (c Comments) Declared() bool {
	return len(c.Line) > 0 || len(c.Block) > 0
}

// BlockPairs は、宣言されたブロックの対を [開き, 閉じ] の並びにする（検証を通った設定を前提に、
// 2つ揃った対だけを返す）。
func (c Comments) BlockPairs() [][2]string {
	pairs := make([][2]string, 0, len(c.Block))
	for _, b := range c.Block {
		if len(b) == 2 {
			pairs = append(pairs, [2]string{b[0], b[1]})
		}
	}
	return pairs
}

// Allow は許可する器1つ。ここに列挙されなかった器のコメントは、中身が何であれ違反。
type Allow struct {
	Place string `yaml:"place"`

	// Kind は、この器で許すコメントの種別。省略時は全 kind。
	Kind []string `yaml:"kind"`

	// Label は、この器のコメントの頭に立つ札。指定するとラベル必須になる。
	Label []string `yaml:"label"`

	// Form は器の中の書式。省略時は書式を問わない。
	Form *Form `yaml:"form"`
}

// Form は器の中の書式。形だけを見る。語彙は見ない。省略したものは検査しない。
type Form struct {
	// Subject は、1行目が紐づく宣言の名前で始まることを求めるか（required | off）。
	Subject string `yaml:"subject"`

	// Headings は、見出しを書けるか（deny | allow）。
	Headings string `yaml:"headings"`

	// Paragraphs は段落数の上限。
	Paragraphs *int `yaml:"paragraphs"`

	// MaxLines は行数の上限。
	MaxLines *int `yaml:"max_lines"`

	// URLs は、URL を書けるか（deny | allow）。
	URLs string `yaml:"urls"`
}

// Rule は層2（語彙）のルール。語彙を持つのは設定ファイルだけで、ツールのコードは実行時の既定を
// 持たない（init が書き込むプリセットは、生成された時点でプロジェクトのものになる）。
type Rule struct {
	ID      string `yaml:"id"`
	Pattern string `yaml:"pattern"`
	Message string `yaml:"message"`
	Where   Where  `yaml:"where"`

	// Regexp は、Load が Pattern から組み立てたもの。不正な正規表現は設定エラー。
	Regexp *regexp.Regexp `yaml:"-"`
}

// Where は層2 のルールの適用範囲。省略した軸は絞らない。
type Where struct {
	// Syntax は、絞り込む syntax エントリ名（と予約値 text）。! 始まりで除外。
	Syntax []string `yaml:"syntax"`

	Kind []string `yaml:"kind"`

	// Path は、絞り込むパスの glob。! 始まりで除外。
	Path []string `yaml:"path"`
}

// SelectsSyntax は、where.syntax がその面を選ぶかを見る。「!」始まりは除外であり、正のエントリに
// 当たっていても除外に当たれば落とす（除外がいつも勝つので、並べる順は結果を変えない）。除外だけを
// 並べた並び（["!text"]）は、それ以外のすべてを選ぶ——syntax の値域は syntax: のキーと予約値 text で
// 閉じているので、「これ以外」が曖昧さなく決まる。where.path の glob は開いた空間なので、除外だけの
// 並びは何も選ばない（glob.Selects）。
func (w Where) SelectsSyntax(name string) bool {
	positives := 0
	for _, s := range w.Syntax {
		if ex, ok := strings.CutPrefix(s, "!"); ok {
			if ex == name {
				return false
			}
			continue
		}
		positives++
	}
	if positives == 0 {
		return true
	}
	return slices.Contains(w.Syntax, name)
}

// FamilyOf は、syntax エントリ name のファミリ。family: を省いたエントリは、キーがファミリ名
// そのもの（syntax.hash は family: hash）。
func (c *Config) FamilyOf(name string) string {
	if f := c.Syntax[name].Family; f != "" {
		return f
	}
	return name
}

// Error は設定エラー。設定が読めない・スキーマに合わない・正規表現が不正、のいずれか。
// CLI はこれを終了コード 2 に写し、違反あり（1）と区別する。
type Error struct {
	Path     string
	Problems []string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: config error", e.Path)
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p)
	}
	return b.String()
}

// Load は esorp.yaml を読み、スキーマを検証する。問題は1つ目で打ち切らず、すべて挙げて返す。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Path: path, Problems: []string{err.Error()}}
	}
	return parse(data, path)
}

// TemplateConfig は、esorp init が生成するテンプレートを設定として読む。init --diff が、手元の
// 設定と比べる相手として使う。
func TemplateConfig() (*Config, error) {
	return parse([]byte(Template), "template")
}

func parse(data []byte, path string) (*Config, error) {
	var cfg Config
	if err := yaml.UnmarshalWithOptions(data, &cfg, yaml.Strict()); err != nil {
		return nil, &Error{Path: path, Problems: []string{strings.TrimSpace(err.Error())}}
	}

	if problems := cfg.validate(); len(problems) > 0 {
		return nil, &Error{Path: path, Problems: problems}
	}
	return &cfg, nil
}

func (c *Config) validate() []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if len(c.Syntax) == 0 {
		add("syntax: is empty, so there is nothing to inspect")
	}
	if c.Review != nil && strings.TrimSpace(c.Review.Question) == "" {
		add("review.question: required. With no question, drop review: entirely (layer 3 stays shut)")
	}
	for _, name := range slices.Sorted(maps.Keys(c.Syntax)) {
		if name == SyntaxText {
			add("syntax.%s: %q is the reserved name for input that needs no extraction. It has no files to pick up, so it cannot be an entry here (to narrow a rule to it, write where.syntax: [%s])", name, SyntaxText, SyntaxText)
			continue
		}
		validateSyntax(name, c.FamilyOf(name), c.Syntax[name], add)
	}

	seen := map[string]bool{}
	for i, r := range c.Rules {
		at := fmt.Sprintf("rules[%d]", i)
		switch {
		case r.ID == "":
			add("%s.id: required", at)
		case seen[r.ID]:
			add("%s.id: %q is a duplicate", at, r.ID)
		case slices.Contains(knownViolations, r.ID):
			add("%s.id: %q is a layer 1 violation id. Both severity and disposition look up by id, so the two cannot share one", at, r.ID)
		default:
			seen[r.ID] = true
		}
		if r.Message == "" {
			add("%s.message: required (what to do about the violation)", at)
		}
		if r.Pattern == "" {
			add("%s.pattern: required", at)
		} else if re, err := regexp.Compile(r.Pattern); err != nil {
			add("%s.pattern: invalid regular expression: %v", at, err)
		} else {
			c.Rules[i].Regexp = re
		}
		for _, s := range r.Where.Syntax {
			name, _ := strings.CutPrefix(s, "!")
			if name == SyntaxText {
				continue
			}
			if _, ok := c.Syntax[name]; !ok {
				add("%s.where.syntax: %q is not a name in syntax: (%q is the reserved name for input that needs no extraction)", at, name, SyntaxText)
			}
		}
		for _, k := range r.Where.Kind {
			if _, ok := scan.ParseKind(k); !ok {
				add("%s.where.kind: %q is not a known kind", at, k)
			}
		}
		if len(r.Where.Path) > 0 {
			validateGlobs(at+".where.path", r.Where.Path, add)
		}
	}

	for _, id := range slices.Sorted(maps.Keys(c.Disposition)) {
		if !slices.Contains(knownViolations, id) {
			add("disposition.%s: not a known violation id (%s)", id, strings.Join(knownViolations, " / "))
		}
	}

	for _, id := range slices.Sorted(maps.Keys(c.Severity)) {
		if !slices.Contains(knownViolations, id) && !seen[id] {
			add("severity.%s: not a known violation id (layer 1: %s, or a rules[].id)", id, strings.Join(knownViolations, " / "))
		}
		if v := c.Severity[id]; !slices.Contains(knownSeverities, v) {
			add("severity.%s: %q is not a known strength (%s)", id, v, strings.Join(knownSeverities, " / "))
		}
	}
	return problems
}

func validateSyntax(name, family string, s Syntax, add func(string, ...any)) {
	at := "syntax." + name

	if s.Comments.Declared() {
		validateComments(at, s, add)
	} else {
		if !slices.Contains(knownFamilies, family) {
			add("%s.family: there is no scanner that reads %q (what exists today: %s)", at, family, strings.Join(knownFamilies, " / "))
		}
		validateLang(at, family, s.Lang, add)
	}
	validateFiles(at, s.Files, add)

	switch s.Mode {
	case "structural":
		for i, a := range s.Allow {
			validateAllow(fmt.Sprintf("%s.allow[%d]", at, i), a, add)
		}
	case "content-only":
		if len(s.Allow) > 0 {
			add("%s.allow: mode: content-only does not ask which vessel a comment is in, so this cannot be written", at)
		}
	case "":
		add("%s.mode: required (structural | content-only)", at)
	default:
		add("%s.mode: %q is not known (structural | content-only)", at, s.Mode)
	}
}

// validateLang は lang: の名前を検める。ファミリと食い違う名前（family: hash に lang: go）は
// 設定エラーにする。通してしまうと、コメント記号からして違う字句でそのファイル群を読み、
// 検査されないまま適合したように見える。
func validateLang(at, family, lang string, add func(string, ...any)) {
	if lang == "" {
		return
	}
	f, ok := scan.LangFamily(lang)
	if !ok {
		add("%s.lang: there is no lexer named %q (what exists today: %s)", at, lang, strings.Join(scan.LangNames(), " / "))
		return
	}
	if f != family {
		add("%s.lang: %q is a lexer of the %s family, which contradicts family: %s", at, lang, f, family)
	}
}

// validateComments は、宣言したコメント記法を検める。宣言できるのは mode: content-only のときだけ
// （structural は器の判定に宣言の解析が要る）、lang: / family: とは併記できない（どちらも読み方の
// 宣言で、食い違う）。ブロックの対は開きと閉じの2つで、どちらも空にはできない。
func validateComments(at string, s Syntax, add func(string, ...any)) {
	if s.Mode != "" && s.Mode != "content-only" {
		add("%s.comments: can only be declared under mode: content-only (structural needs a parse of the declarations to tell vessels apart)", at)
	}
	if s.Lang != "" {
		add("%s.comments: cannot be written alongside lang: (both declare how to read, and they contradict)", at)
	}
	if s.Family != "" {
		add("%s.comments: cannot be written alongside family: (comments: decides how to read, so family has no effect)", at)
	}
	for i, l := range s.Comments.Line {
		if l == "" {
			add("%s.comments.line[%d]: an empty marker cannot be written", at, i)
		}
	}
	for i, b := range s.Comments.Block {
		if len(b) != 2 {
			add(`%s.comments.block[%d]: written as the opener and the closer, the two of them (["/*", "*/"])`, at, i)
			continue
		}
		if b[0] == "" || b[1] == "" {
			add("%s.comments.block[%d]: neither the opener nor the closer can be empty", at, i)
		}
	}
}

// validateFiles は files: の glob を検める。
func validateFiles(at string, files []string, add func(string, ...any)) {
	if len(files) == 0 {
		add("%s.files: required (the globs of the files this entry's scanner reads)", at)
		return
	}
	validateGlobs(at+".files", files, add)
}

// validateGlobs は、パスを絞る glob の並びを検める（syntax.files: と rules[].where.path: が通る）。
// 「!」始まりは除外。不正な glob はどのパスにも当たらないので、通してしまうと、そのファイル群は
// 検査されないまま適合したように見える。除外だけを並べた並びも同じ（何も選ばない）。
func validateGlobs(at string, globs []string, add func(string, ...any)) {
	positives := 0
	for i, g := range globs {
		pat, excluded := strings.CutPrefix(g, "!")
		switch {
		case pat == "":
			add("%s[%d]: an empty glob", at, i)
		case !glob.Valid(pat):
			add("%s[%d]: %q is not a valid glob", at, i, g)
		case !excluded:
			positives++
		}
	}
	if positives == 0 {
		add(`%s: exclusions alone (the ones starting with !) match no file at all (to carve them out of everything, write "**" alongside)`, at)
	}
}

func validateAllow(at string, a Allow, add func(string, ...any)) {
	p, ok := place.Parse(a.Place)
	if a.Place == "" {
		add("%s.place: required", at)
	} else if !ok {
		add("%s.place: %q is not a known place class (header / doc / trailing / leading / orphan)", at, a.Place)
	}

	for _, k := range a.Kind {
		if _, ok := scan.ParseKind(k); !ok {
			add("%s.kind: %q is not a known kind (line / block / docline / docblock)", at, k)
		}
	}
	for _, l := range a.Label {
		if l == "" {
			add("%s.label: an empty label cannot be written", at)
		}
	}

	if a.Form == nil {
		return
	}
	f := a.Form
	if f.Subject != "" {
		if !slices.Contains([]string{"required", "off"}, f.Subject) {
			add("%s.form.subject: %q is not known (required | off)", at, f.Subject)
		} else if ok && p != place.Doc {
			add("%s.form.subject: can only be given under place: doc (with no declaration attached, there is no name to check the first line against)", at)
		}
	}
	for _, sw := range []struct {
		key, val string
	}{
		{"headings", f.Headings},
		{"urls", f.URLs},
	} {
		if sw.val != "" && !slices.Contains([]string{"deny", "allow"}, sw.val) {
			add("%s.form.%s: %q is not known (deny | allow)", at, sw.key, sw.val)
		}
	}
	if f.Paragraphs != nil && *f.Paragraphs < 1 {
		add("%s.form.paragraphs: %d cannot be a cap on the number of paragraphs (1 or more)", at, *f.Paragraphs)
	}
	if f.MaxLines != nil && *f.MaxLines < 1 {
		add("%s.form.max_lines: %d cannot be a cap on the number of lines (1 or more)", at, *f.MaxLines)
	}
}

// PlaceValue / KindValues は、検証を通った設定の文字列を値にする。
func (a Allow) PlaceValue() place.Place {
	p, _ := place.Parse(a.Place)
	return p
}

func (a Allow) KindValues() []scan.Kind {
	return kindValues(a.Kind)
}

// KindValues は、ルールの where.kind を値にする。
func (w Where) KindValues() []scan.Kind {
	return kindValues(w.Kind)
}

func kindValues(kinds []string) []scan.Kind {
	var out []scan.Kind
	for _, k := range kinds {
		v, _ := scan.ParseKind(k)
		out = append(out, v)
	}
	return out
}
