package scan

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CStyle は cstyle ファミリ（// と /* */ ＋ doc 記法）の字句解析器。
// 言語差はすべて spec が持ち、この関数自体は言語を知らない。
//
// 不正なソース（閉じていないブロックコメントや文字列）でもエラーにせず、
// そのトークンを EOF まで伸ばして返す。監査ツールであって、コンパイラではない。
func CStyle(src []byte, spec LangSpec) []Token {
	s := &cstyleScanner{src: src, spec: spec, line: 1}
	return s.scan()
}

type cstyleScanner struct {
	src       []byte
	spec      LangSpec
	toks      []Token
	pos       int
	line      int
	lineStart int // 現在行の先頭のバイト位置。列を出すのに使う
}

func (s *cstyleScanner) scan() []Token {
	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == '\n':
			s.newline()
		case c == ' ' || c == '\t' || c == '\r':
			s.pos++
		case s.tryComment():
		case s.tryString():
		default:
			s.wordOrPunct()
		}
	}
	return s.toks
}

func (s *cstyleScanner) tryComment() bool {
	// doc 記法は行/ブロックコメント記法を接頭辞に含む（/// は // で始まる）ので、先に照合する。
	for _, p := range s.spec.DocLine {
		if s.hasDoc(p) {
			s.lineComment(KindDocLine)
			return true
		}
	}
	// /**/ は空のブロックコメントであって doc ではない（TS の /** に食わせない）。
	if !s.has(s.spec.BlockOpen + s.spec.BlockClose) {
		for _, p := range s.spec.DocBlock {
			if s.hasDoc(p) {
				s.blockComment(KindDocBlock)
				return true
			}
		}
	}
	if s.spec.LineComment != "" && s.has(s.spec.LineComment) {
		s.lineComment(KindLine)
		return true
	}
	if s.spec.BlockOpen != "" && s.has(s.spec.BlockOpen) {
		s.blockComment(KindBlock)
		return true
	}
	return false
}

func (s *cstyleScanner) lineComment(kind Kind) {
	start, line, col := s.pos, s.line, s.col()
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
	text := strings.TrimSuffix(string(s.src[start:s.pos]), "\r")
	s.emit(kind, line, col, line, text)
}

func (s *cstyleScanner) blockComment(kind Kind) {
	start, line, col := s.pos, s.line, s.col()
	s.pos += len(s.spec.BlockOpen)
	depth := 1

	for s.pos < len(s.src) && depth > 0 {
		switch {
		case s.src[s.pos] == '\n':
			s.newline()
		case s.spec.BlockNests && s.has(s.spec.BlockOpen):
			s.pos += len(s.spec.BlockOpen)
			depth++
		case s.has(s.spec.BlockClose):
			s.pos += len(s.spec.BlockClose)
			depth--
		default:
			s.pos++
		}
	}
	s.emit(kind, line, col, s.line, string(s.src[start:s.pos]))
}

func (s *cstyleScanner) tryString() bool {
	for _, sp := range s.spec.Strings {
		n, close, ok := sp.openAt(s.src, s.pos)
		if !ok {
			continue
		}
		if sp.OneRune && !s.oneRune(sp, n, close) {
			continue
		}
		s.stringLit(sp, n, close)
		return true
	}
	return false
}

// oneRune は、開きの引用符が1文字（またはエスケープ列1つ）だけを囲んで閉じるかを見る。これを
// 見ずに引用符から次の引用符までを文字列にすると、Rust の fn f<'a>(x: &'a str) の 'a>(x: &' が
// 文字列になり、行の後ろにコメントがあれば、それごと飲み込む。エスケープ列は長さが変わる
// （\n / \u{1F600}）ので、同じ行で閉じることだけを見る。
func (s *cstyleScanner) oneRune(sp StringSpec, n int, close string) bool {
	p := s.pos + n
	if p >= len(s.src) || s.src[p] == '\n' {
		return false
	}
	if sp.Escape && s.src[p] == '\\' {
		for p++; p < len(s.src) && s.src[p] != '\n'; p++ {
			if hasAt(s.src, p, close) {
				return true
			}
		}
		return false
	}
	_, size := utf8.DecodeRune(s.src[p:])
	return hasAt(s.src, p+size, close)
}

// stringLit は、開きの長さ n と、それに対応する閉じ記号を受けて文字列リテラルを1つ読む
// （閉じ記号が開きに依るのは Rust の r#"…"# のような可変長の区切りがあるため）。
func (s *cstyleScanner) stringLit(sp StringSpec, n int, close string) {
	start, line, col := s.pos, s.line, s.col()
	s.pos += n

	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			if !sp.Multiline {
				// 閉じずに行が終わった。不正なソースなので、行末で打ち切る。
				s.emit(KindString, line, col, s.line, string(s.src[start:s.pos]))
				return
			}
			s.newline()
		case sp.Escape && c == '\\' && s.pos+1 < len(s.src):
			s.pos++
			if s.src[s.pos] == '\n' {
				s.newline()
			} else {
				s.pos++
			}
		case s.has(close):
			s.pos += len(close)
			s.emit(KindString, line, col, s.line, string(s.src[start:s.pos]))
			return
		default:
			s.pos++
		}
	}
	s.emit(KindString, line, col, s.line, string(s.src[start:s.pos]))
}

func (s *cstyleScanner) wordOrPunct() {
	line, col, start := s.line, s.col(), s.pos

	r, size := utf8.DecodeRune(s.src[s.pos:])
	if !isWordRune(r) {
		s.pos += size
		s.emit(KindPunct, line, col, line, string(s.src[start:s.pos]))
		return
	}
	for s.pos < len(s.src) {
		r, size = utf8.DecodeRune(s.src[s.pos:])
		if !isWordRune(r) {
			break
		}
		s.pos += size
	}
	s.emit(KindWord, line, col, line, string(s.src[start:s.pos]))
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// has は、現在位置が p で始まるかを見る（p が空なら常に偽）。
func (s *cstyleScanner) has(p string) bool {
	return hasAt(s.src, s.pos, p)
}

// hasDoc は、現在位置が doc 記法 p に当たるかを見る。記号を重ねた区切り線（//// … ）は
// doc ではないので、記法の最後の1字がそのまま続くなら当たったことにしない（当てると、
// 区切り線が doc の器を名乗り、そこが逃げ場になる）。
func (s *cstyleScanner) hasDoc(p string) bool {
	if !s.has(p) {
		return false
	}
	next := s.pos + len(p)
	return next >= len(s.src) || s.src[next] != p[len(p)-1]
}

// hasAt は、src の pos が p で始まるかを見る（p が空なら常に偽）。
func hasAt(src []byte, pos int, p string) bool {
	if p == "" || pos < 0 || len(src)-pos < len(p) {
		return false
	}
	return string(src[pos:pos+len(p)]) == p
}

func (s *cstyleScanner) col() int {
	return s.pos - s.lineStart + 1
}

func (s *cstyleScanner) newline() {
	s.pos++
	s.line++
	s.lineStart = s.pos
}

func (s *cstyleScanner) emit(kind Kind, line, col, endLine int, text string) {
	s.toks = append(s.toks, Token{Kind: kind, Line: line, Col: col, EndLine: endLine, Text: text})
}
