package scan

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CStyle は cstyle ファミリ（// と /* */ ＋ doc 記法）の字句解析器。言語差はすべて spec が持ち、
// この関数自体は言語を知らない。不正なソース（閉じていないブロックコメントや文字列）でもエラーに
// せず、そのトークンを EOF まで伸ばして返す。監査ツールであって、コンパイラではない。
func CStyle(src []byte, spec LangSpec) []Token {
	s := &cstyleScanner{src: src, spec: spec, line: 1}
	return s.scan()
}

type cstyleScanner struct {
	src  []byte
	spec LangSpec
	toks []Token
	pos  int
	line int

	// lineStart は現在行の先頭のバイト位置。列を出すのに使う。
	lineStart int
}

func (s *cstyleScanner) scan() []Token {
	for s.pos < len(s.src) {
		s.scanOnce()
	}
	return s.toks
}

// scanOnce は、現在位置からトークンを1つ読む（補間の中もここを通る）。
func (s *cstyleScanner) scanOnce() {
	switch c := s.src[s.pos]; {
	case c == '\n':
		s.newline()
	case c == ' ' || c == '\t' || c == '\r':
		s.pos++
	case s.tryComment():
	case s.tryString():
	case s.tryRegex():
	case s.tryJSX():
	default:
		s.wordOrPunct()
	}
}

// tryComment は、現在位置がコメントの開きなら、その1つを読む。doc 記法は行/ブロックコメント記法を
// 接頭辞に含む（/// は // で始まる）ので先に照合し、開いてすぐ閉じるもの（/**/）は空のブロック
// コメントであって doc ではないので、doc 記法に食わせない。
func (s *cstyleScanner) tryComment() bool {
	for _, p := range s.spec.DocLine {
		if s.hasDoc(p) {
			s.lineComment(KindDocLine)
			return true
		}
	}
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
		if sp.Interp != "" {
			s.template(sp, n)
			return true
		}
		s.stringLit(sp, n, close)
		return true
	}
	return false
}

// template は、補間を持つ文字列（TS のテンプレートリテラル）を読む。${ … } の中は再びコード
// なので、文字列として飲み込むことはできない（中にコメントも文字列も現れうるし、そこに現れた
// 「}」がテンプレートを閉じるとも限らない）。そこで、文字列の部分を片ごとに出し、補間の中は
// スキャナ本体を回し直して読む。テンプレートの入れ子は、その再帰で解ける。
func (s *cstyleScanner) template(sp StringSpec, n int) {
	start, line, col := s.pos, s.line, s.col()
	s.pos += n

	for s.pos < len(s.src) {
		switch {
		case s.src[s.pos] == '\n':
			s.newline()
		case sp.Escape && s.src[s.pos] == '\\' && s.pos+1 < len(s.src):
			s.pos++
			if s.src[s.pos] == '\n' {
				s.newline()
			} else {
				s.pos++
			}
		case s.has(sp.Interp):
			s.pos += len(sp.Interp)
			s.emit(KindString, line, col, s.line, string(s.src[start:s.pos]))
			s.interp()
			start, line, col = s.pos, s.line, s.col()
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

// interp は、補間の中をコードとして読み、対応する「}」の手前で戻る（その「}」は、続く
// 文字列の片の先頭になる）。中括弧の深さは、コメントと文字列を読み飛ばしてから数える。
func (s *cstyleScanner) interp() {
	depth := 1
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '}':
			if depth--; depth == 0 {
				return
			}
		case '{':
			depth++
		}
		s.scanOnce()
	}
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
// （閉じ記号が開きに依るのは Rust の r#"…"# のような可変長の区切りがあるため）。改行を含められない
// 形が閉じずに行末に来たら、不正なソースなので、そこで打ち切る。
func (s *cstyleScanner) stringLit(sp StringSpec, n int, close string) {
	start, line, col := s.pos, s.line, s.col()
	s.pos += n

	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\n':
			if !sp.Multiline {
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

// tryRegex は、現在位置が正規表現リテラルの開きなら、それを1つ読む。引用符を含む正規表現
// （/don't/）を文字列の開きと読むと、行の後ろにある本物のコメントを文字列の中に飲み込む。
// 「/」は除算演算子でもあるので、開きかどうかは直前のトークンで見分ける（コメントの // と /* は、
// ここに来る前に読まれている）。
func (s *cstyleScanner) tryRegex() bool {
	if !s.spec.Regex || s.src[s.pos] != '/' || !s.exprCanStart() {
		return false
	}
	m := s.snapshot()
	if s.regexLit() {
		return true
	}
	s.restore(m)
	return false
}

// regexLit は正規表現リテラルを1つ読む。閉じの「/」は、エスケープされておらず、文字クラス […] の
// 中でもないもの。改行までに閉じなければ、それは正規表現ではないので偽を返す（呼び手が読み直す）。
func (s *cstyleScanner) regexLit() bool {
	start, line, col := s.pos, s.line, s.col()
	s.pos++
	inClass := false

	for s.pos < len(s.src) {
		switch c := s.src[s.pos]; {
		case c == '\n':
			return false
		case c == '\\' && s.pos+1 < len(s.src) && s.src[s.pos+1] != '\n':
			s.pos += 2
		case c == '[':
			inClass = true
			s.pos++
		case c == ']':
			inClass = false
			s.pos++
		case c == '/' && !inClass:
			s.pos++
			for s.pos < len(s.src) && s.src[s.pos] >= 'a' && s.src[s.pos] <= 'z' {
				s.pos++
			}
			s.emit(KindString, line, col, line, string(s.src[start:s.pos]))
			return true
		default:
			s.pos++
		}
	}
	return false
}

// exprCanStart は、現在位置から式が始まりうるかを、直前のトークンで見る。式が終わった直後
// （識別子・数値・文字列・閉じ括弧）に来る「/」は除算であり、「<」は比較かジェネリクスであって、
// リテラルの開きではない。開き括弧・演算子・式を導くキーワード（return …）の後なら、開きでありうる。
func (s *cstyleScanner) exprCanStart() bool {
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

// lastCode は、直前の非コメントトークンを返す（無ければ nil）。
func (s *cstyleScanner) lastCode() *Token {
	for i := len(s.toks) - 1; i >= 0; i-- {
		if !s.toks[i].Kind.IsComment() {
			return &s.toks[i]
		}
	}
	return nil
}

// mark はスキャナの状態の控え。読み直し（正規表現・JSX の当たりが外れたとき）に使う。
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
