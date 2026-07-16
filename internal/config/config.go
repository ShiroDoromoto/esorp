// Package config は、esorp.yaml の読み込みとスキーマ検証を担う。
//
// 設定ファイルが唯一の真実であり、ツールは実行時の既定を持たない（D-4）。書かれていないものは
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

// Config は esorp.yaml の全体。
type Config struct {
	Syntax      map[string]Syntax `yaml:"syntax"`
	Disposition map[string]string `yaml:"disposition"`
	Rules       []Rule            `yaml:"rules"`
	Review      *Review           `yaml:"review"`
	Baseline    string            `yaml:"baseline"`

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
	Syntax []string `yaml:"syntax"`
	Kind   []string `yaml:"kind"`

	// Path は、絞り込むパスの glob。! 始まりで除外。
	Path []string `yaml:"path"`
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
	fmt.Fprintf(&b, "%s: 設定エラー", e.Path)
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
	return parse([]byte(Template), "テンプレート")
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
		add("syntax: が空です。検査するファイルがありません")
	}
	if c.Review != nil && strings.TrimSpace(c.Review.Question) == "" {
		add("review.question: 必須です。問いが無いなら、review: ごと消してください（層3 は開きません）")
	}
	for _, name := range slices.Sorted(maps.Keys(c.Syntax)) {
		if name == SyntaxText {
			add("syntax.%s: %q は取り出しの無い入力を指す予約値です。files を持たない入力なので、拾うエントリは書けません（ルールを絞るなら where.syntax: [%s]）", name, SyntaxText, SyntaxText)
			continue
		}
		validateSyntax(name, c.FamilyOf(name), c.Syntax[name], add)
	}

	seen := map[string]bool{}
	for i, r := range c.Rules {
		at := fmt.Sprintf("rules[%d]", i)
		switch {
		case r.ID == "":
			add("%s.id: 必須です", at)
		case seen[r.ID]:
			add("%s.id: %q が重複しています", at, r.ID)
		case slices.Contains(knownViolations, r.ID):
			add("%s.id: %q は層1 の違反 id です。baseline も disposition も id で引くので、重ねられません", at, r.ID)
		default:
			seen[r.ID] = true
		}
		if r.Message == "" {
			add("%s.message: 必須です（違反時に提示する始末のしかた）", at)
		}
		if r.Pattern == "" {
			add("%s.pattern: 必須です", at)
		} else if re, err := regexp.Compile(r.Pattern); err != nil {
			add("%s.pattern: 正規表現が不正です: %v", at, err)
		} else {
			c.Rules[i].Regexp = re
		}
		for _, s := range r.Where.Syntax {
			if s == SyntaxText {
				continue
			}
			if _, ok := c.Syntax[s]; !ok {
				add("%s.where.syntax: %q は syntax: に無い名前です（%q は取り出しの無い入力を指す予約値）", at, s, SyntaxText)
			}
		}
		for _, k := range r.Where.Kind {
			if _, ok := scan.ParseKind(k); !ok {
				add("%s.where.kind: %q は不明な kind です", at, k)
			}
		}
		if len(r.Where.Path) > 0 {
			validateGlobs(at+".where.path", r.Where.Path, add)
		}
	}

	for _, id := range slices.Sorted(maps.Keys(c.Disposition)) {
		if !slices.Contains(knownViolations, id) {
			add("disposition.%s: 不明な違反 id です（%s）", id, strings.Join(knownViolations, " / "))
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
			add("%s.family: %q を読むスキャナがありません（今あるのは %s）", at, family, strings.Join(knownFamilies, " / "))
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
			add("%s.allow: mode: content-only では器を問わないので、書けません", at)
		}
	case "":
		add("%s.mode: 必須です（structural | content-only）", at)
	default:
		add("%s.mode: %q は不明です（structural | content-only）", at, s.Mode)
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
		add("%s.lang: %q という字句はありません（今あるのは %s）", at, lang, strings.Join(scan.LangNames(), " / "))
		return
	}
	if f != family {
		add("%s.lang: %q は %s ファミリの字句です（family: %s と食い違います）", at, lang, f, family)
	}
}

// validateComments は、宣言したコメント記法を検める。宣言できるのは mode: content-only のときだけ
// （structural は器の判定に宣言の解析が要る）、lang: / family: とは併記できない（どちらも読み方の
// 宣言で、食い違う）。ブロックの対は開きと閉じの2つで、どちらも空にはできない。
func validateComments(at string, s Syntax, add func(string, ...any)) {
	if s.Mode != "" && s.Mode != "content-only" {
		add("%s.comments: mode: content-only のときだけ宣言できます（structural は器の判定に宣言の解析が要ります）", at)
	}
	if s.Lang != "" {
		add("%s.comments: lang: とは併記できません（どちらも読み方の宣言で、食い違います）", at)
	}
	if s.Family != "" {
		add("%s.comments: family: とは併記できません（comments: が読み方を決めるので、family は効きません）", at)
	}
	for i, l := range s.Comments.Line {
		if l == "" {
			add("%s.comments.line[%d]: 空の記号は書けません", at, i)
		}
	}
	for i, b := range s.Comments.Block {
		if len(b) != 2 {
			add(`%s.comments.block[%d]: 開きと閉じの2つで書きます（["/*", "*/"]）`, at, i)
			continue
		}
		if b[0] == "" || b[1] == "" {
			add("%s.comments.block[%d]: 開きも閉じも空にはできません", at, i)
		}
	}
}

// validateFiles は files: の glob を検める。
func validateFiles(at string, files []string, add func(string, ...any)) {
	if len(files) == 0 {
		add("%s.files: 必須です（このファミリのスキャナで読むファイルの glob）", at)
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
			add("%s[%d]: 空の glob です", at, i)
		case !glob.Valid(pat):
			add("%s[%d]: %q は glob として不正です", at, i, g)
		case !excluded:
			positives++
		}
	}
	if positives == 0 {
		add(`%s: 除外（! 始まり）だけでは、どのファイルにも当たりません（全体から除くなら "**" を併せて書きます）`, at)
	}
}

func validateAllow(at string, a Allow, add func(string, ...any)) {
	p, ok := place.Parse(a.Place)
	if a.Place == "" {
		add("%s.place: 必須です", at)
	} else if !ok {
		add("%s.place: %q は不明な位置クラスです（header / doc / trailing / leading / orphan）", at, a.Place)
	}

	for _, k := range a.Kind {
		if _, ok := scan.ParseKind(k); !ok {
			add("%s.kind: %q は不明な kind です（line / block / docline / docblock）", at, k)
		}
	}
	for _, l := range a.Label {
		if l == "" {
			add("%s.label: 空のラベルは書けません", at)
		}
	}

	if a.Form == nil {
		return
	}
	f := a.Form
	if f.Subject != "" {
		if !slices.Contains([]string{"required", "off"}, f.Subject) {
			add("%s.form.subject: %q は不明です（required | off）", at, f.Subject)
		} else if ok && p != place.Doc {
			add("%s.form.subject: place: doc のときだけ指定できます（紐づく宣言が無ければ、名前で始まるかを判定できません）", at)
		}
	}
	for _, sw := range []struct {
		key, val string
	}{
		{"headings", f.Headings},
		{"urls", f.URLs},
	} {
		if sw.val != "" && !slices.Contains([]string{"deny", "allow"}, sw.val) {
			add("%s.form.%s: %q は不明です（deny | allow）", at, sw.key, sw.val)
		}
	}
	if f.Paragraphs != nil && *f.Paragraphs < 1 {
		add("%s.form.paragraphs: %d は段落数の上限になりません（1 以上）", at, *f.Paragraphs)
	}
	if f.MaxLines != nil && *f.MaxLines < 1 {
		add("%s.form.max_lines: %d は行数の上限になりません（1 以上）", at, *f.MaxLines)
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
