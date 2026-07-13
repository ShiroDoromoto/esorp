package config

import (
	"fmt"
	"sort"
	"strings"
)

// Section は差分の1かたまり。見出しと、その下に並ぶ行。
type Section struct {
	Title string
	Lines []string
}

// Diff は、現行テンプレートと手元の設定の差分を返す。空なら差分なし。設定は生成された時点で
// ユーザーのものなので、ここでは差を見せるだけで、取り込むかどうかには関与しない。syntax エントリの
// 対応づけを名前ではなく「ファミリが同じで、見るファイルが重なること」で行うのは、名前で照合すると、
// 言語ごとに分けたテンプレートのエントリ（cstyle-go / cstyle-rust）と手元の1本のエントリ（cstyle）が、
// 丸ごと追加・削除として出てしまい、読めないため。
func Diff(local, tmpl *Config) []Section {
	var out []Section

	pairs, onlyTmpl, onlyLocal := pairSyntax(local, tmpl)
	for _, p := range pairs {
		lines := diffSyntax(local.Syntax[p.local], tmpl.Syntax[p.tmpl])
		if len(lines) == 0 {
			continue
		}
		title := fmt.Sprintf("syntax.%s（テンプレートの %s と対応）", p.local, p.tmpl)
		out = append(out, Section{Title: title, Lines: lines})
	}

	if len(onlyTmpl) > 0 {
		var lines []string
		for _, name := range onlyTmpl {
			lines = append(lines, fmt.Sprintf("%s: %s", name, strings.Join(tmpl.Syntax[name].Files, " ")))
		}
		out = append(out, Section{
			Title: "テンプレートだけにある syntax エントリ（使わない言語なら、無いままで構いません）",
			Lines: lines,
		})
	}
	if len(onlyLocal) > 0 {
		var lines []string
		for _, name := range onlyLocal {
			lines = append(lines, fmt.Sprintf("%s: %s", name, strings.Join(local.Syntax[name].Files, " ")))
		}
		out = append(out, Section{
			Title: "手元だけにある syntax エントリ（あなたが足したもの）",
			Lines: lines,
		})
	}

	if lines := diffFiles(local, tmpl); len(lines) > 0 {
		out = append(out, Section{Title: "見にいくファイル", Lines: lines})
	}

	if lines := diffMap(local.Disposition, tmpl.Disposition); len(lines) > 0 {
		out = append(out, Section{Title: "disposition（違反時に提示する始末のしかた）", Lines: lines})
	}
	if lines := diffRules(local.Rules, tmpl.Rules); len(lines) > 0 {
		out = append(out, Section{Title: "rules（層2 の語彙。テンプレートは既定を持たない）", Lines: lines})
	}

	var misc []string
	if local.RespectGitignore != tmpl.RespectGitignore {
		misc = append(misc, field("respect_gitignore",
			fmt.Sprint(local.RespectGitignore), fmt.Sprint(tmpl.RespectGitignore)))
	}
	if local.Baseline != tmpl.Baseline {
		misc = append(misc, field("baseline", local.Baseline, tmpl.Baseline))
	}
	if len(misc) > 0 {
		out = append(out, Section{Title: "その他", Lines: misc})
	}

	return out
}

// pair は、対応づいた syntax エントリの組。
type pair struct{ local, tmpl string }

// pairSyntax は、手元とテンプレートの syntax エントリを、ファミリと見るファイルの重なりで対応づける。
// 手元の1本が複数のテンプレートエントリに対応することがある（cstyle が cstyle-go / cstyle-rust の
// 両方を兼ねている状態）。それは対応づけの失敗ではなく、手元の設定がそう書かれているという事実。
func pairSyntax(local, tmpl *Config) (pairs []pair, onlyTmpl, onlyLocal []string) {
	matched := map[string]bool{}

	for _, tn := range names(tmpl.Syntax) {
		var found string
		for _, ln := range names(local.Syntax) {
			if local.FamilyOf(ln) != tmpl.FamilyOf(tn) {
				continue
			}
			if !overlaps(local.Syntax[ln].Files, tmpl.Syntax[tn].Files) {
				continue
			}
			found = ln
			break
		}
		if found == "" {
			onlyTmpl = append(onlyTmpl, tn)
			continue
		}
		matched[found] = true
		pairs = append(pairs, pair{local: found, tmpl: tn})
	}

	for _, ln := range names(local.Syntax) {
		if !matched[ln] {
			onlyLocal = append(onlyLocal, ln)
		}
	}
	return pairs, onlyTmpl, onlyLocal
}

// overlaps は、2つの files が同じファイルを1つでも見にいくかを、glob の文字列一致で見る。除外
// （「!」始まり）は、見にいく先を足さないので数えない。
func overlaps(a, b []string) bool {
	for _, x := range a {
		if strings.HasPrefix(x, "!") {
			continue
		}
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// diffFiles は、見にいくファイルの差を設定の全体で1回だけ出す。エントリごとに出すと、手元の1本が
// 複数のテンプレートエントリを兼ねているとき（cstyle が go / rust / ts を1本で見ている）、他の
// エントリが拾っている glob まで「手元だけ」「テンプレートだけ」として何度も並び、読めなくなる。
func diffFiles(local, tmpl *Config) []string {
	globs := func(c *Config) []string {
		var out []string
		for _, name := range names(c.Syntax) {
			for _, g := range c.Syntax[name].Files {
				if !contains(out, g) {
					out = append(out, g)
				}
			}
		}
		return out
	}

	add, del := setDiff(globs(local), globs(tmpl))
	var lines []string
	if len(add) > 0 {
		lines = append(lines, "テンプレートだけ: "+strings.Join(add, " "))
	}
	if len(del) > 0 {
		lines = append(lines, "手元だけ: "+strings.Join(del, " "))
	}
	return lines
}

func diffSyntax(l, t Syntax) []string {
	var lines []string

	if l.Mode != t.Mode {
		lines = append(lines, field("mode", l.Mode, t.Mode))
	}
	if l.Lang != t.Lang {
		lines = append(lines, field("lang", l.Lang, t.Lang))
	}

	lp, tp := byPlace(l.Allow), byPlace(t.Allow)
	for _, p := range places(lp, tp) {
		la, inLocal := lp[p]
		ta, inTmpl := tp[p]
		switch {
		case !inLocal:
			lines = append(lines, fmt.Sprintf("allow[%s]  テンプレートだけにあります（この器を許可していません）", p))
		case !inTmpl:
			lines = append(lines, fmt.Sprintf("allow[%s]  手元だけにあります（テンプレートは、この器を許可しません）", p))
		default:
			lines = append(lines, diffAllow(p, la, ta)...)
		}
	}
	return lines
}

func diffAllow(p string, l, t Allow) []string {
	var lines []string

	if add, del := setDiff(l.Kind, t.Kind); len(add) > 0 || len(del) > 0 {
		lines = append(lines, field(fmt.Sprintf("allow[%s].kind", p), list(l.Kind), list(t.Kind)))
	}
	if add, del := setDiff(l.Label, t.Label); len(add) > 0 || len(del) > 0 {
		lines = append(lines, field(fmt.Sprintf("allow[%s].label", p), list(l.Label), list(t.Label)))
	}

	lf, tf := l.Form, t.Form
	if lf == nil {
		lf = &Form{}
	}
	if tf == nil {
		tf = &Form{}
	}
	for _, f := range []struct {
		name    string
		l, t    string
		isEmpty bool
	}{
		{name: "subject", l: lf.Subject, t: tf.Subject},
		{name: "headings", l: lf.Headings, t: tf.Headings},
		{name: "refs", l: lf.Refs, t: tf.Refs},
		{name: "urls", l: lf.URLs, t: tf.URLs},
		{name: "paragraphs", l: num(lf.Paragraphs), t: num(tf.Paragraphs)},
		{name: "max_lines", l: num(lf.MaxLines), t: num(tf.MaxLines)},
	} {
		if f.l != f.t {
			lines = append(lines, field(fmt.Sprintf("allow[%s].form.%s", p, f.name), f.l, f.t))
		}
	}
	return lines
}

func diffMap(l, t map[string]string) []string {
	var lines []string
	seen := map[string]bool{}
	var keys []string
	for k := range l {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range t {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		lv, tv := strings.TrimSpace(l[k]), strings.TrimSpace(t[k])
		if lv == tv {
			continue
		}
		switch {
		case lv == "":
			lines = append(lines, fmt.Sprintf("%s  テンプレートだけにあります", k))
		case tv == "":
			lines = append(lines, fmt.Sprintf("%s  手元だけにあります", k))
		default:
			lines = append(lines, fmt.Sprintf("%s  文言が違います", k))
		}
	}
	return lines
}

func diffRules(l, t []Rule) []string {
	byID := func(rs []Rule) map[string]Rule {
		m := map[string]Rule{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	lm, tm := byID(l), byID(t)

	var lines []string
	for _, r := range t {
		if _, ok := lm[r.ID]; !ok {
			lines = append(lines, fmt.Sprintf("%s  テンプレートだけにあります: %s", r.ID, r.Pattern))
		}
	}
	for _, r := range l {
		tr, ok := tm[r.ID]
		if !ok {
			lines = append(lines, fmt.Sprintf("%s  手元だけにあります（あなたの選択）: %s", r.ID, r.Pattern))
			continue
		}
		if tr.Pattern != r.Pattern {
			lines = append(lines, field(r.ID+".pattern", r.Pattern, tr.Pattern))
		}
	}
	return lines
}

// field は、1つの値の差を「手元 / テンプレート」の並びで出す。空の値は「（無し）」と書く——
// 値が違うのか、そもそも書かれていないのかは、取り込むかどうかの判断で意味が変わる。
func field(name, local, tmpl string) string {
	return fmt.Sprintf("%s  手元: %s  テンプレート: %s", name, or(local), or(tmpl))
}

func or(v string) string {
	if v == "" {
		return "（無し）"
	}
	return v
}

func num(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(*v)
}

func list(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return "[" + strings.Join(vs, " ") + "]"
}

// setDiff は、b だけにあるもの（add）と a だけにあるもの（del）を、順序を保って返す。
func setDiff(a, b []string) (add, del []string) {
	in := func(vs []string, v string) bool {
		for _, x := range vs {
			if x == v {
				return true
			}
		}
		return false
	}
	for _, v := range b {
		if !in(a, v) {
			add = append(add, v)
		}
	}
	for _, v := range a {
		if !in(b, v) {
			del = append(del, v)
		}
	}
	return add, del
}

func byPlace(as []Allow) map[string]Allow {
	m := map[string]Allow{}
	for _, a := range as {
		m[a.Place] = a
	}
	return m
}

// places は、器を設定に書かれうる順（外から内へ）で返す。map の反復順は回るたびに変わるので、
// そのままでは差分の並びが安定しない。
func places(ms ...map[string]Allow) []string {
	order := []string{"header", "doc", "leading", "trailing", "orphan"}
	seen, rest := map[string]bool{}, []string{}
	for _, m := range ms {
		for p := range m {
			if seen[p] {
				continue
			}
			seen[p] = true
			if !contains(order, p) {
				rest = append(rest, p)
			}
		}
	}
	sort.Strings(rest)

	var out []string
	for _, p := range order {
		if seen[p] {
			out = append(out, p)
		}
	}
	return append(out, rest...)
}

func contains(vs []string, v string) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}

func names(m map[string]Syntax) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
