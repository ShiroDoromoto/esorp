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

	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// v0 のスキャナが持つ構文ファミリ。他のファミリ（hash / sgml / cssblock）は、
// そのスキャナを実装するまで設定に書けない（書けてしまうと、黙って何も検査しない）。
var knownFamilies = []string{"cstyle"}

// 層1 が出す違反 id。disposition のキーはこれでなければならない。
var knownViolations = []string{
	"place-not-allowed", "label-required",
	"form-subject", "form-headings", "form-paragraphs", "form-refs", "form-max-lines", "form-urls",
}

// Config は esorp.yaml の全体。
type Config struct {
	Syntax      map[string]Syntax `yaml:"syntax"`
	Disposition map[string]string `yaml:"disposition"`
	Rules       []Rule            `yaml:"rules"`
	Baseline    string            `yaml:"baseline"`
}

// Syntax は構文ファミリごとのエントリ。キーが cstyle のようなファミリ名そのものであれば
// Family は省略でき、本体は違う設定で使い分けるとき（cstyle-src / cstyle-test）に指す。
type Syntax struct {
	Family string   `yaml:"family"`
	Files  []string `yaml:"files"` // 拡張子ではなく glob（Makefile のように拡張子を持たないものがある）
	Mode   string   `yaml:"mode"`  // structural | content-only
	Allow  []Allow  `yaml:"allow"` // mode: structural のときだけ書ける
}

// Allow は許可する器1つ。ここに列挙されなかった器のコメントは、中身が何であれ違反。
type Allow struct {
	Place string   `yaml:"place"`
	Kind  []string `yaml:"kind"`  // 省略時は全 kind
	Label []string `yaml:"label"` // 指定するとラベル必須になる
	Form  *Form    `yaml:"form"`  // 省略時は書式を問わない
}

// Form は器の中の書式。形だけを見る。語彙は見ない。省略したものは検査しない。
type Form struct {
	Subject    string `yaml:"subject"`    // required | off
	Headings   string `yaml:"headings"`   // deny | allow
	Paragraphs *int   `yaml:"paragraphs"` // 段落数の上限
	Refs       string `yaml:"refs"`       // deny | allow
	MaxLines   *int   `yaml:"max_lines"`  // 行数の上限
	URLs       string `yaml:"urls"`       // deny | allow
}

// Rule は層2（語彙）のルール。v0 では読み込んで検証するだけで、効かせるのは後続。
type Rule struct {
	ID      string `yaml:"id"`
	Pattern string `yaml:"pattern"`
	Message string `yaml:"message"`
	Where   Where  `yaml:"where"`

	Regexp *regexp.Regexp `yaml:"-"` // Load が組み立てる。不正な正規表現は設定エラー
}

// Where は層2 のルールの適用範囲。省略した軸は絞らない。
type Where struct {
	Syntax []string `yaml:"syntax"`
	Kind   []string `yaml:"kind"`
	Path   []string `yaml:"path"` // ! 始まりで除外
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

	var cfg Config
	// 未知のキーを拒否する。綴り違いが黙って無視されると、設定ファイルが唯一の真実でなくなる。
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
	for _, name := range slices.Sorted(maps.Keys(c.Syntax)) {
		validateSyntax(name, c.Syntax[name], add)
	}

	seen := map[string]bool{}
	for i, r := range c.Rules {
		at := fmt.Sprintf("rules[%d]", i)
		switch {
		case r.ID == "":
			add("%s.id: 必須です", at)
		case seen[r.ID]:
			add("%s.id: %q が重複しています", at, r.ID)
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
			if _, ok := c.Syntax[s]; !ok {
				add("%s.where.syntax: %q は syntax: に無い名前です", at, s)
			}
		}
		for _, k := range r.Where.Kind {
			if _, ok := scan.ParseKind(k); !ok {
				add("%s.where.kind: %q は不明な kind です", at, k)
			}
		}
	}

	for _, id := range slices.Sorted(maps.Keys(c.Disposition)) {
		if !slices.Contains(knownViolations, id) {
			add("disposition.%s: 不明な違反 id です（%s）", id, strings.Join(knownViolations, " / "))
		}
	}
	return problems
}

func validateSyntax(name string, s Syntax, add func(string, ...any)) {
	at := "syntax." + name

	family := s.Family
	if family == "" {
		family = name // キーがファミリ名そのもの
	}
	if !slices.Contains(knownFamilies, family) {
		add("%s.family: %q を読むスキャナがありません（今あるのは %s）", at, family, strings.Join(knownFamilies, " / "))
	}
	if len(s.Files) == 0 {
		add("%s.files: 必須です（このファミリのスキャナで読むファイルの glob）", at)
	}

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
			// subject は紐づく宣言が無ければ判定できない。
			add("%s.form.subject: place: doc のときだけ指定できます", at)
		}
	}
	for _, sw := range []struct {
		key, val string
	}{
		{"headings", f.Headings},
		{"refs", f.Refs},
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
	var out []scan.Kind
	for _, k := range a.Kind {
		v, _ := scan.ParseKind(k)
		out = append(out, v)
	}
	return out
}
