package rule

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// 層1（書式）が出す違反 id。
const (
	FormSubject    = "form-subject"
	FormHeadings   = "form-headings"
	FormParagraphs = "form-paragraphs"
	FormRefs       = "form-refs"
	FormMaxLines   = "form-max-lines"
	FormURLs       = "form-urls"
)

// 見出しは「# … 」の形（Markdown）。#123 は追跡番号であって見出しではないので、
// 記号のあとに空白を要求して切り分ける。
var headingRe = regexp.MustCompile(`(?m)^#{1,6}[ \t]`)

// 追跡番号への参照。#123 と ABC-123 の2形だけを見る。形の判定であって、語彙判定ではない。
var refRe = regexp.MustCompile(`#\d+|\b[A-Z][A-Z0-9]*-\d+\b`)

var urlRe = regexp.MustCompile(`https?://`)

// Form は、器を通ったコメントの中の書式を検査する。
//
// 形だけを見る。語彙は見ない。allow に form: が無ければ、書式は問わない（何も検査しない）。
// 器の違反が出たコメントはここに来ない（順序は 器 → 書式 → 語彙）。
func Form(c place.Comment, f *config.Form, disp map[string]string, spec scan.LangSpec) []Violation {
	if f == nil {
		return nil
	}
	body := scan.Body(c.Text, spec)

	var out []Violation
	add := func(id string) {
		out = append(out, *violation(id, c, disp))
	}

	// subject は紐づく宣言が無ければ判定できない。宣言名を取り出せなかったコメント
	// （var ( … ) のような塊、宣言に紐づかない doc 記法）は、検査しない。
	if f.Subject == "required" && c.Decl != "" && !startsWithDecl(body, c.Decl) {
		add(FormSubject)
	}
	if f.Headings == "deny" && headingRe.MatchString(body) {
		add(FormHeadings)
	}
	if f.Paragraphs != nil && paragraphs(body) > *f.Paragraphs {
		add(FormParagraphs)
	}
	if f.Refs == "deny" && refRe.MatchString(body) {
		add(FormRefs)
	}
	if f.MaxLines != nil && lines(body) > *f.MaxLines {
		add(FormMaxLines)
	}
	if f.URLs == "deny" && urlRe.MatchString(body) {
		add(FormURLs)
	}
	return out
}

// startsWithDecl は、本文の1行目が宣言の名前で始まるかを見る。
//
// 名前の直後が識別子の続きであってはならない。Open で始まることと OpenFile で始まることは違う
// （後者は目の前の宣言の説明ではない）。
func startsWithDecl(body, decl string) bool {
	first, _, _ := strings.Cut(body, "\n")
	rest, ok := strings.CutPrefix(first, decl)
	if !ok {
		return false
	}
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}

// paragraphs は、空行で区切られた塊の数を数える。
func paragraphs(body string) int {
	n := 0
	in := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			in = false
			continue
		}
		if !in {
			n++
			in = true
		}
	}
	return n
}

func lines(body string) int {
	if body == "" {
		return 0
	}
	return strings.Count(body, "\n") + 1
}
