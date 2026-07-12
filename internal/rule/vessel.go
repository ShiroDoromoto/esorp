// Package rule は、コメントを設定に照らして評価する。
//
// 順序は 器 → 書式 → 語彙で、先に落ちたら後は見ない。置き場所が違うコメントに、形の違反まで
// 重ねて出してもノイズにしかならない。
package rule

import (
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
	ID      string
	Line    int
	Col     int
	Place   place.Place
	Kind    scan.Kind
	Text    string // コメントの生テキスト
	Message string
}

// Vessel は、コメントの器を allow の列挙に照らす。
//
// 列挙されなかった器のコメントは、中身が何であれ違反。違反が無ければ、その器を許した allow を
// 返す（書式の検査は、その allow の form を使う）。
func Vessel(c place.Comment, allows []config.Allow, disp map[string]string, spec scan.LangSpec) (*config.Allow, *Violation) {
	var matched []config.Allow
	for _, a := range allows {
		if a.PlaceValue() != c.Place {
			continue
		}
		// kind の省略は「全 kind」の意味。
		if kinds := a.KindValues(); len(kinds) > 0 && !slices.Contains(kinds, c.Kind) {
			continue
		}
		matched = append(matched, a)
	}

	if len(matched) == 0 {
		return nil, violation(PlaceNotAllowed, c, disp)
	}

	// 器を許す allow が複数あれば、どれか1つが受け入れれば通る。
	body := scan.Body(c.Text, spec)
	for _, a := range matched {
		if len(a.Label) == 0 || hasLabel(body, a.Label) {
			return &a, nil
		}
	}
	return nil, violation(LabelRequired, c, disp)
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

func violation(id string, c place.Comment, disp map[string]string) *Violation {
	return &Violation{
		ID:      id,
		Line:    c.Line,
		Col:     c.Col,
		Place:   c.Place,
		Kind:    c.Kind,
		Text:    c.Text,
		Message: disp[id],
	}
}
