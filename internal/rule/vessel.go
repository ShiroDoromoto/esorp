// Package rule は、コメントを設定に照らして評価する。
//
// 順序は 器 → 書式 → 語彙で、先に落ちたら後は見ない。置き場所が違うコメントに、形の違反まで
// 重ねて出してもノイズにしかならない。
package rule

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// 層1（器）が出す違反 id。
const (
	PlaceNotAllowed = "place-not-allowed"
	LabelRequired   = "label-required"
)

// Violation は違反1件。Message は disposition から引いた文字列をそのまま持つ。
// ツールは行き先を規定せず、設定された文字列を提示するだけ。
type Violation struct {
	ID    string
	Line  int
	Col   int
	Place place.Place
	Kind  scan.Kind

	// Text はコメントの生テキスト。
	Text string

	Message string

	// Site は、この違反を決めた設定の場所。
	Site Site
}

// Site は、違反を決めた設定の場所。違反 id と設定（allow の列挙 / form / rules）は一対一で
// 対応しているので、報告した違反から設定の該当箇所を指せる（esorp explain）。
type Site struct {
	// Path は設定の中での場所（syntax.cstyle.allow[1].form.paragraphs / rules[0]）。
	Path string

	// Allow は、この違反を決めた allow の添字。器の列挙そのものを指す place-not-allowed と、
	// 層2 の違反では -1。
	Allow int

	// Rule は、この違反を出した rules の添字。層1 の違反では -1。
	Rule int
}

// vesselSite は、層1 の違反を決めた設定の場所を組み立てる。allow が -1 なら器の列挙そのもの
// （どの allow も受けなかった）、key が空なら allow エントリそのものを指す。
func vesselSite(t Target, allow int, key string) Site {
	path := "syntax." + t.Syntax + ".allow"
	if allow >= 0 {
		path += fmt.Sprintf("[%d]", allow)
	}
	if key != "" {
		path += "." + key
	}
	return Site{Path: path, Allow: allow, Rule: -1}
}

// Vessel は、コメントの器を allow の列挙に照らす。列挙されなかった器のコメントは、中身が何であれ
// 違反。違反が無ければ、その器を許した allow の添字を返す（書式の検査は、その allow の form を使う）。
// kind を省いた allow は全 kind を受け、器を許す allow が複数あれば、どれか1つが受け入れれば通る。
func Vessel(c place.Comment, allows []config.Allow, disp map[string]string, t Target, spec scan.LangSpec) (int, *Violation) {
	var matched []int
	for i, a := range allows {
		if a.PlaceValue() != c.Place {
			continue
		}
		if kinds := a.KindValues(); len(kinds) > 0 && !slices.Contains(kinds, c.Kind) {
			continue
		}
		matched = append(matched, i)
	}

	if len(matched) == 0 {
		return -1, violation(PlaceNotAllowed, vesselSite(t, -1, ""), c, disp)
	}

	body := scan.Body(c.Text, spec)
	for _, i := range matched {
		if a := allows[i]; len(a.Label) == 0 || hasLabel(body, a.Label) {
			return i, nil
		}
	}
	return -1, violation(LabelRequired, vesselSite(t, matched[0], "label"), c, disp)
}

// hasLabel は、本文の先頭が設定されたラベルのいずれかで始まるかを見る。
// 判定はこれだけ。中身の意味は読まない。
func hasLabel(body string, labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(body, l) {
			return true
		}
	}
	return false
}

func violation(id string, site Site, c place.Comment, disp map[string]string) *Violation {
	return &Violation{
		ID:      id,
		Line:    c.Line,
		Col:     c.Col,
		Place:   c.Place,
		Kind:    c.Kind,
		Text:    c.Text,
		Message: disp[id],
		Site:    site,
	}
}
