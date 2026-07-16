package config

import (
	"fmt"
	"sort"
	"strings"
)

// Section は差分の1かたまり。見出しと、その下に並ぶ差分。
type Section struct {
	Title   string
	Changes []Change
}

// Change は差分1つ。Key は設定の該当箇所まで辿れる名前（syntax.cstyle.allow[doc].kind）で、
// Local / Tmpl はその値、Only は片方にしか無いこと（"local" / "template"）、Text は人向けの1行。
// 組み立て済みの文だけにせず、キーと値に分けて持つのは、差分を読むのが人とはかぎらないため
// （取り込むかどうかを決めるのは相変わらず読み手で、esorp は設定を書き換えない）。
type Change struct {
	Key   string
	Local string
	Tmpl  string
	Only  string
	Text  string
}

// Lines は、このかたまりの差分を人向けの行にする。
func (s Section) Lines() []string {
	out := make([]string, 0, len(s.Changes))
	for _, c := range s.Changes {
		out = append(out, c.Text)
	}
	return out
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
		changes := diffSyntax("syntax."+p.local, local.Syntax[p.local], tmpl.Syntax[p.tmpl])
		if len(changes) == 0 {
			continue
		}
		title := fmt.Sprintf("syntax.%s (paired with %s in the template)", p.local, p.tmpl)
		out = append(out, Section{Title: title, Changes: changes})
	}

	if len(onlyTmpl) > 0 {
		out = append(out, Section{
			Title:   "syntax entries only in the template (for a language you do not use, not having it is fine)",
			Changes: onlySyntax(tmpl, onlyTmpl, "template"),
		})
	}
	if len(onlyLocal) > 0 {
		out = append(out, Section{
			Title:   "syntax entries only in yours (ones you added)",
			Changes: onlySyntax(local, onlyLocal, "local"),
		})
	}

	if changes := diffFiles(local, tmpl); len(changes) > 0 {
		out = append(out, Section{Title: "the files being looked at", Changes: changes})
	}

	if changes := diffMap("disposition", local.Disposition, tmpl.Disposition); len(changes) > 0 {
		out = append(out, Section{Title: "disposition (what to do about a violation)", Changes: changes})
	}
	if changes := diffSeverity(local.Severity, tmpl.Severity); len(changes) > 0 {
		out = append(out, Section{Title: "severity (how hard each violation is enforced. An id not written down is enforce)", Changes: changes})
	}
	if changes := diffRules(local.Rules, tmpl.Rules); len(changes) > 0 {
		out = append(out, Section{Title: "rules (the layer 2 lexicon. The presets are a starting point — drop them or add to them freely)", Changes: changes})
	}

	var misc []Change
	if local.RespectGitignore != tmpl.RespectGitignore {
		misc = append(misc, changed("respect_gitignore", "respect_gitignore",
			fmt.Sprint(local.RespectGitignore), fmt.Sprint(tmpl.RespectGitignore)))
	}
	if len(misc) > 0 {
		out = append(out, Section{Title: "the rest", Changes: misc})
	}

	return out
}

// changed は、両方にあって値が違う項目の Change。key は設定の該当箇所まで辿れる名前、label は
// 人向けの1行に出す短い名前（見出しがエントリを名乗っているので、行では繰り返さない）。
func changed(key, label, local, tmpl string) Change {
	return Change{Key: key, Local: local, Tmpl: tmpl, Text: field(label, local, tmpl)}
}

// onlySyntax は、片方にしかない syntax エントリの Change。値はそのエントリが見にいくファイル。
func onlySyntax(c *Config, entries []string, only string) []Change {
	changes := make([]Change, 0, len(entries))
	for _, name := range entries {
		files := strings.Join(c.Syntax[name].Files, " ")
		ch := Change{
			Key:  "syntax." + name,
			Only: only,
			Text: fmt.Sprintf("%s: %s", name, files),
		}
		if only == "local" {
			ch.Local = files
		} else {
			ch.Tmpl = files
		}
		changes = append(changes, ch)
	}
	return changes
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
func diffFiles(local, tmpl *Config) []Change {
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
	var changes []Change
	if len(add) > 0 {
		changes = append(changes, Change{
			Key:  "files",
			Only: "template",
			Tmpl: strings.Join(add, " "),
			Text: "template only: " + strings.Join(add, " "),
		})
	}
	if len(del) > 0 {
		changes = append(changes, Change{
			Key:   "files",
			Only:  "local",
			Local: strings.Join(del, " "),
			Text:  "yours only: " + strings.Join(del, " "),
		})
	}
	return changes
}

func diffSyntax(key string, l, t Syntax) []Change {
	var changes []Change

	if l.Mode != t.Mode {
		changes = append(changes, changed(key+".mode", "mode", l.Mode, t.Mode))
	}
	if l.Lang != t.Lang {
		changes = append(changes, changed(key+".lang", "lang", l.Lang, t.Lang))
	}

	lp, tp := byPlace(l.Allow), byPlace(t.Allow)
	for _, p := range places(lp, tp) {
		la, inLocal := lp[p]
		ta, inTmpl := tp[p]
		allow := fmt.Sprintf("allow[%s]", p)
		switch {
		case !inLocal:
			changes = append(changes, Change{
				Key:  key + "." + allow,
				Only: "template",
				Text: allow + "  in the template only (you do not allow this vessel)",
			})
		case !inTmpl:
			changes = append(changes, Change{
				Key:  key + "." + allow,
				Only: "local",
				Text: allow + "  in yours only (the template does not allow this vessel)",
			})
		default:
			changes = append(changes, diffAllow(key, p, la, ta)...)
		}
	}
	return changes
}

func diffAllow(key, p string, l, t Allow) []Change {
	var changes []Change
	at := func(name string) (string, string) {
		label := fmt.Sprintf("allow[%s].%s", p, name)
		return key + "." + label, label
	}

	if add, del := setDiff(l.Kind, t.Kind); len(add) > 0 || len(del) > 0 {
		k, label := at("kind")
		changes = append(changes, changed(k, label, list(l.Kind), list(t.Kind)))
	}
	if add, del := setDiff(l.Label, t.Label); len(add) > 0 || len(del) > 0 {
		k, label := at("label")
		changes = append(changes, changed(k, label, list(l.Label), list(t.Label)))
	}

	lf, tf := l.Form, t.Form
	if lf == nil {
		lf = &Form{}
	}
	if tf == nil {
		tf = &Form{}
	}
	for _, f := range []struct {
		name string
		l, t string
	}{
		{name: "subject", l: lf.Subject, t: tf.Subject},
		{name: "headings", l: lf.Headings, t: tf.Headings},
		{name: "urls", l: lf.URLs, t: tf.URLs},
		{name: "paragraphs", l: num(lf.Paragraphs), t: num(tf.Paragraphs)},
		{name: "max_lines", l: num(lf.MaxLines), t: num(tf.MaxLines)},
	} {
		if f.l != f.t {
			k, label := at("form." + f.name)
			changes = append(changes, changed(k, label, f.l, f.t))
		}
	}
	return changes
}

// diffMap は、文言の差を「違う」とだけ告げる。中身まで並べないのは、disposition が段落で書かれる
// ためで、両方を全文並べても読めない。
func diffMap(key string, l, t map[string]string) []Change {
	var changes []Change
	for _, k := range unionKeys(l, t) {
		lv, tv := strings.TrimSpace(l[k]), strings.TrimSpace(t[k])
		if lv == tv {
			continue
		}
		ch := Change{Key: key + "." + k, Local: lv, Tmpl: tv}
		switch {
		case lv == "":
			ch.Only = "template"
			ch.Text = fmt.Sprintf("%s  in the template only", k)
		case tv == "":
			ch.Only = "local"
			ch.Text = fmt.Sprintf("%s  in yours only", k)
		default:
			ch.Text = fmt.Sprintf("%s  the wording differs", k)
		}
		changes = append(changes, ch)
	}
	return changes
}

// diffSeverity は、強度の差を値ごと並べる。diffMap が「違う」とだけ告げるのに対してこちらが
// 値を見せるのは、強度が enforce / advisory の2語しかないため——並べても読めるし、どちらへ
// 転ぶかは CI の赤/緑そのものなので、読み手は値を見ないと取り込むかを決められない。
func diffSeverity(l, t map[string]string) []Change {
	var changes []Change
	for _, k := range unionKeys(l, t) {
		lv, tv := l[k], t[k]
		if lv == tv {
			continue
		}
		ch := Change{Key: "severity." + k, Local: lv, Tmpl: tv, Text: field(k, lv, tv)}
		switch {
		case lv == "":
			ch.Only = "template"
		case tv == "":
			ch.Only = "local"
		}
		changes = append(changes, ch)
	}
	return changes
}

// unionKeys は、両方の map のキーを重複なく集めて並べる。
func unionKeys(l, t map[string]string) []string {
	var keys []string
	for k := range l {
		keys = append(keys, k)
	}
	for k := range t {
		if _, ok := l[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func diffRules(l, t []Rule) []Change {
	byID := func(rs []Rule) map[string]Rule {
		m := map[string]Rule{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	lm, tm := byID(l), byID(t)

	var changes []Change
	for _, r := range t {
		if _, ok := lm[r.ID]; !ok {
			changes = append(changes, Change{
				Key:  "rules." + r.ID,
				Only: "template",
				Tmpl: r.Pattern,
				Text: fmt.Sprintf("%s  in the template only: %s", r.ID, r.Pattern),
			})
		}
	}
	for _, r := range l {
		tr, ok := tm[r.ID]
		if !ok {
			changes = append(changes, Change{
				Key:   "rules." + r.ID,
				Only:  "local",
				Local: r.Pattern,
				Text:  fmt.Sprintf("%s  in yours only (your choice): %s", r.ID, r.Pattern),
			})
			continue
		}
		if tr.Pattern != r.Pattern {
			changes = append(changes, changed("rules."+r.ID+".pattern", r.ID+".pattern", r.Pattern, tr.Pattern))
		}
	}
	return changes
}

// field は、1つの値の差を「手元 / テンプレート」の並びで出す。空の値は「(none)」と書く——
// 値が違うのか、そもそも書かれていないのかは、取り込むかどうかの判断で意味が変わる。
func field(name, local, tmpl string) string {
	return fmt.Sprintf("%s  yours: %s  template: %s", name, or(local), or(tmpl))
}

func or(v string) string {
	if v == "" {
		return "(none)"
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
