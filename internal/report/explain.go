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
		if base.Has(f.Key) {
			indent(&b, "この違反は baseline が抑えています（check には出ません）。")
		}

		b.WriteByte('\n')
		fmt.Fprintf(&b, "  決めているのは %s の %s です:\n", configPath, f.Site.Path)
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
		return append(out, fmt.Sprintf("place: %s（kind: %s）はこの列挙にありません。列挙されなかった器のコメントは、中身が何であれ違反です", f.Place, f.Kind))

	case f.ID == rule.LabelRequired:
		return []string{
			"label: [" + strings.Join(allows[f.Site.Allow].Label, ", ") + "]",
			"この器のコメントは、このいずれかで始めてください",
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
		b.WriteString("  form: あり")
	}
	return b.String()
}

// formValue は、当たった書式の指定を、その値ごと書き出す。違反 id と form のキーは一対一。
func formValue(f *config.Form, id string) string {
	switch id {
	case rule.FormSubject:
		return "subject: " + f.Subject
	case rule.FormHeadings:
		return "headings: " + f.Headings
	case rule.FormParagraphs:
		return fmt.Sprintf("paragraphs: %d", *f.Paragraphs)
	case rule.FormRefs:
		return "refs: " + f.Refs
	case rule.FormMaxLines:
		return fmt.Sprintf("max_lines: %d", *f.MaxLines)
	case rule.FormURLs:
		return "urls: " + f.URLs
	default:
		return id
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
