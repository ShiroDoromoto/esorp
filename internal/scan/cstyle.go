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
		if s.has(p) {
			s.lineComment(KindDocLine)
			return true
		}
	}
	// /**/ は空のブロックコメントであって doc ではない（TS の /** に食わせない）。
	if !s.has(s.spec.BlockOpen + s.spec.BlockClose) {
		for _, p := range s.spec.DocBlock {
			if s.has(p) {
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
		if s.has(sp.Open) {
			s.stringLit(sp)
			return true
		}
	}
	return false
}

func (s *cstyleScanner) stringLit(sp StringSpec) {
	start, line, col := s.pos, s.line, s.col()
	s.pos += len(sp.Open)

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
		case s.has(sp.Close):
			s.pos += len(sp.Close)
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
	if p == "" || len(s.src)-s.pos < len(p) {
		return false
	}
	return string(s.src[s.pos:s.pos+len(p)]) == p
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
