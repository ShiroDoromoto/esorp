package scan

import (
	"slices"
	"strings"
)

// tryJSX は、現在位置が要素の開きなら、その要素を丸ごと1つ読む。「<」が要素を開くのか比較・
// ジェネリクスなのかは字句だけでは決まらない（TSX の難所）ので、直前のトークンと次の1字で当たりを
// 付け、閉じタグに届かないまま尽きたら JSX ではなかったとみなして読み直す。当たりが外れても、
// 字句を読み落とさない。
func (s *cstyleScanner) tryJSX() bool {
	if !s.spec.JSX || s.src[s.pos] != '<' || !s.jsxCanOpen() {
		return false
	}
	m := s.snapshot()
	if s.jsxElement() {
		return true
	}
	s.restore(m)
	return false
}

// jsxCanOpen は、この「<」が要素を開きうるかを見る。式が終わった直後（識別子・文字列・閉じ括弧）
// の「<」は比較かジェネリクスであって、要素の開きではない。式がここから始まりうる位置
// （return などのキーワード・開き括弧・演算子）なら、要素でありうる。次の1字が名前の始まりでも
// 「>」（フラグメント）でもなければ、いずれにせよ要素ではない。
func (s *cstyleScanner) jsxCanOpen() bool {
	n := s.pos + 1
	if n >= len(s.src) || (s.src[n] != '>' && !isNameStart(s.src[n])) {
		return false
	}
	prev := s.lastCode()
	if prev == nil {
		return true
	}
	switch prev.Kind {
	case KindWord:
		return slices.Contains(s.spec.ExprKeywords, prev.Text)
	case KindString:
		return false
	case KindPunct:
		return prev.Text != ")" && prev.Text != "]" && prev.Text != "}"
	}
	return true
}

// jsxElement は「<」から要素を1つ読む。閉じないまま尽きたら偽を返す（呼び手が読み直す）。
func (s *cstyleScanner) jsxElement() bool {
	s.wordOrPunct()
	selfClosing, ok := s.jsxOpenTag()
	if !ok {
		return false
	}
	if selfClosing {
		return true
	}
	return s.jsxChildren()
}

// jsxOpenTag は、開きタグの中（タグ名・属性）を「>」または「/>」まで読む。属性の値は文字列か
// コードなので、そこはスキャナ本体を回し直して読む（タグの中に置かれたコメントも、そこで拾う）。
func (s *cstyleScanner) jsxOpenTag() (selfClosing, ok bool) {
	for s.pos < len(s.src) {
		switch {
		case s.has("/>"):
			s.wordOrPunct()
			s.wordOrPunct()
			return true, true
		case s.src[s.pos] == '>':
			s.wordOrPunct()
			return false, true
		case s.src[s.pos] == '{':
			s.jsxBraces()
		case s.src[s.pos] == '<':
			return false, false
		default:
			s.scanOnce()
		}
	}
	return false, false
}

// jsxChildren は、開きタグの後から対応する閉じタグまでを読む。
func (s *cstyleScanner) jsxChildren() bool {
	for {
		s.jsxText()
		if s.pos >= len(s.src) {
			return false
		}
		if s.src[s.pos] == '{' {
			s.jsxBraces()
			continue
		}
		if s.src[s.pos+1] == '/' {
			return s.jsxCloseTag()
		}
		if !s.jsxElement() {
			return false
		}
	}
}

// jsxText は、タグの中身の地の文を「{」か、タグを開く「<」まで読む。ここに字句は無い
// （<p>it's http://example.com</p> の // は行コメントではなく、' は文字列を開かない）。テキストと
// して読まなければ、前者は誤検知になり、後者は行の後ろにある本物のコメントを飲み込む。テキストは
// コードでもコメントでもないので文字列として出し、空白だけの並びは器の判定に何も足さないので出さない。
func (s *cstyleScanner) jsxText() {
	start, line, col := s.pos, s.line, s.col()
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '{' || (c == '<' && s.jsxTagAhead()):
			s.emitText(start, line, col)
			return
		case c == '\n':
			s.newline()
		default:
			s.pos++
		}
	}
	s.emitText(start, line, col)
}

// jsxTagAhead は、現在位置の「<」がタグ（入れ子の要素・フラグメント・閉じタグ）を開くかを見る。
// 続く1字が名前の始まりでも「>」でも「/」でもなければ、それは不等号であってタグではない
// （<p>1 < 2</p> のように、テキストには不等号が現れる）。
func (s *cstyleScanner) jsxTagAhead() bool {
	n := s.pos + 1
	return n < len(s.src) && (s.src[n] == '/' || s.src[n] == '>' || isNameStart(s.src[n]))
}

// jsxCloseTag は「</ … >」を読む。開きタグと名前が一致するかは見ない。一致しないソースはそもそも
// 壊れており、監査ツールが求めるのは、字句を読み落とさないことだけ。
func (s *cstyleScanner) jsxCloseTag() bool {
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == '>':
			s.wordOrPunct()
			return true
		case c == '\n':
			s.newline()
		case c == ' ' || c == '\t' || c == '\r':
			s.pos++
		default:
			s.wordOrPunct()
		}
	}
	return false
}

// jsxBraces は、JSX の中の { … }（属性の値・子の式）をコードとして読む。中は再びコードなので、
// スキャナ本体を回し直す。中括弧の深さは、コメントと文字列を読み飛ばしてから数える。
func (s *cstyleScanner) jsxBraces() {
	s.wordOrPunct()
	depth := 1
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				s.wordOrPunct()
				return
			}
		}
		s.scanOnce()
	}
}

func (s *cstyleScanner) emitText(start, line, col int) {
	text := string(s.src[start:s.pos])
	if strings.TrimSpace(text) == "" {
		return
	}
	s.emit(KindString, line, col, s.line, text)
}

// lastCode は、直前の非コメントトークンを返す（無ければ nil）。
func (s *cstyleScanner) lastCode() *Token {
	for i := len(s.toks) - 1; i >= 0; i-- {
		if !s.toks[i].Kind.IsComment() {
			return &s.toks[i]
		}
	}
	return nil
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// mark はスキャナの状態の控え。読み直しに使う。
type mark struct {
	pos, line, lineStart, toks int
}

func (s *cstyleScanner) snapshot() mark {
	return mark{pos: s.pos, line: s.line, lineStart: s.lineStart, toks: len(s.toks)}
}

func (s *cstyleScanner) restore(m mark) {
	s.pos, s.line, s.lineStart = m.pos, m.line, m.lineStart
	s.toks = s.toks[:m.toks]
}
