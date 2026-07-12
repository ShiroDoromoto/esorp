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

// headingRe は Markdown の見出し（行頭の井桁のあとに空白）に当たる。井桁に数字が続く形は
// 追跡番号であって見出しではないので、空白を要求して切り分ける。
var headingRe = regexp.MustCompile(`(?m)^#{1,6}[ \t]`)

// refRe は追跡番号への参照に当たる。井桁に数字が続く形と、大文字の接頭辞にハイフンと数字が
// 続く形の2つだけを見る。形の判定であって、語彙判定ではない。
var refRe = regexp.MustCompile(`#\d+|\b[A-Z][A-Z0-9]*-\d+\b`)

var urlRe = regexp.MustCompile(`https?://`)

// Form は、器を通ったコメントの中の書式を検査する（器の違反が出たコメントはここに来ない。順序は
// 器 → 書式 → 語彙）。形だけを見る。語彙は見ない。allow に form: が無ければ、書式は問わない
// （何も検査しない）。subject は紐づく宣言が無ければ判定できないので、宣言名を取り出せなかった
// コメント（括弧でまとめた宣言そのもの、宣言に紐づかない doc 記法）は検査しない。
func Form(c place.Comment, f *config.Form, disp map[string]string, spec scan.LangSpec) []Violation {
	if f == nil {
		return nil
	}
	lines := scan.BodyLines(c.Text, spec)
	code := scan.CodeLines(lines, spec)
	body := strings.Join(lines, "\n")

	var out []Violation
	add := func(id string) {
		out = append(out, *violation(id, c, disp))
	}

	if f.Subject == "required" && c.Decl != "" && !startsWithDecl(body, c.Decl) {
		add(FormSubject)
	}
	if f.Headings == "deny" && hasHeading(lines, code) {
		add(FormHeadings)
	}
	if f.Paragraphs != nil && paragraphs(lines, code) > *f.Paragraphs {
		add(FormParagraphs)
	}
	if f.Refs == "deny" && refRe.MatchString(body) {
		add(FormRefs)
	}
	if f.MaxLines != nil && len(lines) > *f.MaxLines {
		add(FormMaxLines)
	}
	if f.URLs == "deny" && urlRe.MatchString(body) {
		add(FormURLs)
	}
	return out
}

// hasHeading は、見出しの行があるかを見る。コードブロックの中の「#」は見出しではない。
func hasHeading(lines []string, code []bool) bool {
	for i, line := range lines {
		if !code[i] && headingRe.MatchString(line) {
			return true
		}
	}
	return false
}

// startsWithDecl は、本文の1行目が宣言の名前で始まるかを見る。名前の直後が識別子の続きであっては
// ならない。Open で始まることと OpenFile で始まることは違う（後者は目の前の宣言の説明ではない）。
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

// paragraphs は、空行で区切られた塊のうち、散文の段落の数を数える。
// コードブロックは散文ではないので数えず、箇条書きは散文として数える。
func paragraphs(lines []string, code []bool) int {
	n := 0
	counted := false

	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			counted = false
			continue
		}
		if !counted && !code[i] {
			counted = true
			n++
		}
	}
	return n
}
