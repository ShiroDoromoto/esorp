package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/baseline"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/rule"
)

// Explain は、1つのコメントに出た違反を、それを決めた設定の該当箇所とともに書く。check の出力
// （どこで・何に反し・何が書かれていて・どう始末するか）に「その判断はどこから来たのか」を足す。
// 違反を「禁止」とだけ伝えると書き手は言い換えて再投稿するので、何がその器を許していないのか・
// どの語彙に当たったのかまで見せる。
func Explain(w io.Writer, cfg *config.Config, configPath string, res *audit.Result, base *baseline.Baseline) error {
	var b strings.Builder
	for _, f := range res.Findings {
		fmt.Fprintf(&b, "%s:%d:%d  %s  place=%s kind=%s\n", f.Path, f.Line, f.Col, f.ID, f.Place, f.Kind)
		indent(&b, f.Text)
		indent(&b, f.Message)
		if f.SeamDependent {
			indent(&b, SeamNote)
		}
		if base.Has(f.Key) {
			indent(&b, "This violation is held down by the baseline (it does not appear in check).")
		}

		b.WriteByte('\n')
		fmt.Fprintf(&b, "  Decided by %s at %s:\n", configPath, f.Site.Path)
		for _, line := range site(cfg, f) {
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// site は、違反を決めた設定の該当箇所を、その中身ごと書き出す。設定ファイルを開かずに、なぜこの
// コメントが違反なのかが分かるところまで見せる。
func site(cfg *config.Config, f audit.Finding) []string {
	if f.Site.Rule >= 0 {
		return lexiconSite(cfg.Rules[f.Site.Rule])
	}

	allows := cfg.Syntax[f.Syntax].Allow
	switch {
	case f.ID == rule.PlaceNotAllowed:
		out := make([]string, 0, len(allows)+1)
		for i, a := range allows {
			out = append(out, fmt.Sprintf("allow[%d]  %s", i, vessel(a)))
		}
		return append(out, fmt.Sprintf("place: %s (kind: %s) is not in this enumeration. A comment in a vessel that was not enumerated is a violation, whatever its content", f.Place, f.Kind))

	case f.ID == rule.LabelRequired:
		return []string{
			"label: [" + strings.Join(allows[f.Site.Allow].Label, ", ") + "]",
			"A comment in this vessel must begin with one of these",
		}

	default:
		return []string{formValue(allows[f.Site.Allow].Form, f.ID)}
	}
}

// vessel は allow エントリ1つを1行に畳む（許可されている器の列挙を見せるため）。
func vessel(a config.Allow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "place: %s", a.Place)
	if len(a.Kind) > 0 {
		fmt.Fprintf(&b, "  kind: [%s]", strings.Join(a.Kind, ", "))
	}
	if len(a.Label) > 0 {
		fmt.Fprintf(&b, "  label: [%s]", strings.Join(a.Label, ", "))
	}
	if a.Form != nil {
		b.WriteString("  form: present")
	}
	return b.String()
}

// formValue は、当たった書式の指定を、その値ごと書き出す。
func formValue(f *config.Form, id string) string {
	key, value := formKey(f, id)
	if value == nil {
		return key
	}
	return fmt.Sprintf("%s: %v", key, value)
}

// formKey は、当たった書式の指定を、設定のキーとその値に割る。違反 id と form のキーは一対一。
func formKey(f *config.Form, id string) (string, any) {
	switch id {
	case rule.FormSubject:
		return "subject", f.Subject
	case rule.FormHeadings:
		return "headings", f.Headings
	case rule.FormParagraphs:
		return "paragraphs", *f.Paragraphs
	case rule.FormRefs:
		return "refs", f.Refs
	case rule.FormMaxLines:
		return "max_lines", *f.MaxLines
	case rule.FormURLs:
		return "urls", f.URLs
	default:
		return id, nil
	}
}

// explain の状態。指した行が違反しているのか、適合しているのか、そもそもコメントが無かったのかは、
// 空の explanations では書き分けられない。
const (
	statusViolated   = "violated"
	statusConforming = "conforming"
	statusNoComment  = "no-comment"
)

// jsonExplanation は、違反1件（check の JSON と同じ形）に、それを決めた設定の場所と中身を添えたもの。
type jsonExplanation struct {
	jsonViolation
	Baselined bool     `json:"baselined"`
	Site      jsonSite `json:"site"`
}

// jsonSite は、違反を決めた設定の該当箇所。違反 id と設定は一対一なので、下の4つのうち1つだけが立つ。
type jsonSite struct {
	// Path は設定の中での場所（syntax.cstyle.allow[1].form.paragraphs / rules[0]）。
	Path string `json:"path"`

	// Syntax は、当たった syntax エントリの名前（層2 の違反では空）。
	Syntax string `json:"syntax,omitempty"`

	// Allow は許可されている器の列挙（place-not-allowed）。列挙されなかった器のコメントは、
	// 中身が何であれ違反。
	Allow []jsonAllow `json:"allow,omitempty"`

	// Label は、その器で許されている札の列挙（label-required）。
	Label []string `json:"label,omitempty"`

	// Form は、当たった書式の指定（form-*）。
	Form *jsonFormKey `json:"form,omitempty"`

	// Rule は、当たった層2 のルール。
	Rule *jsonRule `json:"rule,omitempty"`
}

type jsonAllow struct {
	Place string    `json:"place"`
	Kind  []string  `json:"kind,omitempty"`
	Label []string  `json:"label,omitempty"`
	Form  *jsonForm `json:"form,omitempty"`
}

type jsonForm struct {
	Subject    string `json:"subject,omitempty"`
	Headings   string `json:"headings,omitempty"`
	Paragraphs *int   `json:"paragraphs,omitempty"`
	Refs       string `json:"refs,omitempty"`
	MaxLines   *int   `json:"max_lines,omitempty"`
	URLs       string `json:"urls,omitempty"`
}

// jsonFormKey は、当たった書式の指定1つ。Value は数（paragraphs / max_lines）か文字列。
type jsonFormKey struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type jsonRule struct {
	ID      string    `json:"id"`
	Pattern string    `json:"pattern"`
	Message string    `json:"message"`
	Where   jsonWhere `json:"where"`
}

type jsonWhere struct {
	Syntax []string `json:"syntax,omitempty"`
	Kind   []string `json:"kind,omitempty"`
	Path   []string `json:"path,omitempty"`
}

// ExplainJSON は、Explain と同じ中身を機械可読の形で書く。エージェントは check --format json で
// 違反を読むので、その1件をそのまま explain に渡して根拠まで JSON で引けるようにする。
func ExplainJSON(w io.Writer, cfg *config.Config, configPath, path string, line int, res *audit.Result, base *baseline.Baseline) error {
	out := struct {
		Version      int               `json:"version"`
		Config       string            `json:"config"`
		Target       jsonTarget        `json:"target"`
		Status       string            `json:"status"`
		Explanations []jsonExplanation `json:"explanations"`
	}{
		Version:      1,
		Config:       configPath,
		Target:       jsonTarget{Path: path, Line: line},
		Status:       status(res),
		Explanations: make([]jsonExplanation, 0, len(res.Findings)),
	}
	for _, f := range res.Findings {
		out.Explanations = append(out.Explanations, jsonExplanation{
			jsonViolation: violation(f),
			Baselined:     base.Has(f.Key),
			Site:          jsonSiteOf(cfg, f),
		})
	}

	return encode(w, out)
}

type jsonTarget struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

func status(res *audit.Result) string {
	switch {
	case len(res.Findings) > 0:
		return statusViolated
	case res.Comments == 0:
		return statusNoComment
	default:
		return statusConforming
	}
}

// jsonSiteOf は、違反を決めた設定の該当箇所を、その中身ごと組み立てる（site の JSON 版）。
func jsonSiteOf(cfg *config.Config, f audit.Finding) jsonSite {
	out := jsonSite{Path: f.Site.Path}
	if f.Site.Rule >= 0 {
		r := cfg.Rules[f.Site.Rule]
		out.Rule = &jsonRule{
			ID:      r.ID,
			Pattern: r.Pattern,
			Message: strings.TrimRight(r.Message, "\n"),
			Where:   jsonWhere{Syntax: r.Where.Syntax, Kind: r.Where.Kind, Path: r.Where.Path},
		}
		return out
	}

	out.Syntax = f.Syntax
	allows := cfg.Syntax[f.Syntax].Allow
	switch {
	case f.ID == rule.PlaceNotAllowed:
		out.Allow = make([]jsonAllow, 0, len(allows))
		for _, a := range allows {
			out.Allow = append(out.Allow, jsonAllow{Place: a.Place, Kind: a.Kind, Label: a.Label, Form: form(a.Form)})
		}
	case f.ID == rule.LabelRequired:
		out.Label = allows[f.Site.Allow].Label
	default:
		key, value := formKey(allows[f.Site.Allow].Form, f.ID)
		out.Form = &jsonFormKey{Key: key, Value: value}
	}
	return out
}

func form(f *config.Form) *jsonForm {
	if f == nil {
		return nil
	}
	return &jsonForm{
		Subject:    f.Subject,
		Headings:   f.Headings,
		Paragraphs: f.Paragraphs,
		Refs:       f.Refs,
		MaxLines:   f.MaxLines,
		URLs:       f.URLs,
	}
}

// lexiconSite は、当たった層2 のルールを書き出す。message は既に違反として出ているので繰り返さない。
func lexiconSite(r config.Rule) []string {
	out := []string{
		"id: " + r.ID,
		"pattern: " + r.Pattern,
	}
	for _, w := range []struct {
		key    string
		values []string
	}{
		{"where.syntax", r.Where.Syntax},
		{"where.kind", r.Where.Kind},
		{"where.path", r.Where.Path},
	} {
		if len(w.values) > 0 {
			out = append(out, fmt.Sprintf("%s: [%s]", w.key, strings.Join(w.values, ", ")))
		}
	}
	return out
}
