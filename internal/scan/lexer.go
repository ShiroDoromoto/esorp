package scan

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// scalarIndicator は、ブロックスカラーの見出しに立つ指示子（| > と、その字下げ・切り詰めの修飾）。
// 修飾は数字と符号がどちらの順にも並ぶ（|2- も |-2 も同じ）。
var scalarIndicator = regexp.MustCompile(`^[|>][0-9]*[-+]?[0-9]*$`)

// blockScalarHeader は、その行がブロックスカラーを開く見出しなら、行の字下げを返す。指示子が
// 立つのはキー（key:）か並びの印（-）の直後だけで、ただの値として現れる「|」（cmd: echo |）は
// 見出しではない。
func blockScalarHeader(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !scalarIndicator.MatchString(fields[len(fields)-1]) {
		return "", false
	}
	if prev := fields[len(fields)-2]; prev != "-" && !strings.HasSuffix(prev, ":") {
		return "", false
	}
	return indentOf(line), true
}

func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// Scan はソースをトークンに分ける。ファミリ（cstyle / hash / sgml / cssblock）の差も言語差も
// すべて spec が持ち、この関数自体はどちらも知らない。ファミリが違っても仕事は同じ——コメントと
// 文字列を見分けること——であり、違うのは記号と、記号がコメントを開く条件だけ。不正なソース
// （閉じていないブロックコメントや文字列）でもエラーにせず、そのトークンを EOF まで伸ばして返す。
// 監査ツールであって、コンパイラではない。
func Scan(src []byte, spec LangSpec) []Token {
	s := &lexer{src: src, spec: spec, line: 1}
	return s.scan()
}

type lexer struct {
	src  []byte
	spec LangSpec
	toks []Token
	pos  int
	line int

	// lineStart は現在行の先頭のバイト位置。列を出すのに使う。
	lineStart int

	// pending は、この行で開いたヒアドキュメント。中身が始まるのは次の行なので、行を読み終えて
	// から、開いた順に読む。1行で2つ開ける（cat <<A <<B）。
	pending []heredoc
}

// heredoc は、開いたヒアドキュメント1つ。Dash は <<- で開いたもので、閉じの区切りの前にタブを
// 置ける。
type heredoc struct {
	Delim string
	Dash  bool
}

func (s *lexer) scan() []Token {
	for s.pos < len(s.src) {
		s.scanOnce()
	}
	return s.toks
}

// scanOnce は、現在位置からトークンを1つ読む（補間の中もここを通る）。
func (s *lexer) scanOnce() {
	switch c := s.src[s.pos]; {
	case c == '\n':
		start, line := s.lineStart, s.line
		s.newline()
		if s.spec.BlockScalars {
			s.blockScalar(string(s.src[start:s.pos-1]), line)
		}
		for _, h := range s.pending {
			s.heredocBody(h)
		}
		s.pending = nil
	case c == ' ' || c == '\t' || c == '\r':
		s.pos++
	case s.tryShebang():
	case s.tryComment():
	case s.tryString():
	case s.tryHeredoc():
	case s.tryRegex():
	case s.tryJSX():
	default:
		s.wordOrPunct()
	}
}

// tryShebang は、ファイルの1行目に立つ shebang（#!/bin/sh）を1つのトークンとして読む。「#」で
// コメントを開く言語では、これをコメントとして読むと、直後に置かれた冒頭のコメントと1つの器に
// 繋がり、header をこれが占めてしまう。shebang はカーネルへの指示であって、本文でもコードでもない。
// ただし「#!」で始まるだけでは足りず、Rust の内側属性（#![allow(…)]）も同じ2字で始まるので、
// 後ろに実行するものへのパスが続くこと——空白を挟んでもよい「/」——まで見て分ける。
func (s *lexer) tryShebang() bool {
	if s.pos != 0 || !s.has("#!") {
		return false
	}
	p := s.pos + 2
	for p < len(s.src) && (s.src[p] == ' ' || s.src[p] == '\t') {
		p++
	}
	if p >= len(s.src) || s.src[p] != '/' {
		return false
	}

	start, line, col := s.pos, s.line, s.col()
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
	s.emit(KindShebang, line, col, line, strings.TrimSuffix(string(s.src[start:s.pos]), "\r"))
	return true
}

// tryComment は、現在位置がコメントの開きなら、その1つを読む。doc 記法は行/ブロックコメント記法を
// 接頭辞に含む（/// は // で始まる）ので先に照合し、開いてすぐ閉じるもの（/**/）は空のブロック
// コメントであって doc ではないので、doc 記法に食わせない。
func (s *lexer) tryComment() bool {
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
	if s.lineCommentOpens() {
		s.lineComment(KindLine)
		return true
	}
	if s.spec.BlockOpen != "" && s.has(s.spec.BlockOpen) {
		s.blockComment(KindBlock)
		return true
	}
	return false
}

// lineCommentOpens は、現在位置の行コメント記号がコメントを開くかを見る。「//」はどこに現れても
// 開くが、「#」は語の中にも現れる（シェルの ${x#y}）ので、行頭か空白の直後でなければ開かない。
// gitignore はさらに狭く、行頭だけ（行中の「#」はパターンの一部）。
func (s *lexer) lineCommentOpens() bool {
	if s.spec.LineComment == "" || !s.has(s.spec.LineComment) {
		return false
	}
	if s.pos == s.lineStart {
		return true
	}
	if s.spec.LineCommentAtLineStart {
		return false
	}
	if !s.spec.LineCommentSpaced {
		return true
	}
	c := s.src[s.pos-1]
	return c == ' ' || c == '\t' || c == '\r'
}

// blockScalar は、いま読み終えた行がブロックスカラーの見出し（key: | / - >-）なら、その中身を
// 読み飛ばす。中身はコメント記号を含みうるただの文字列であって、コードでもコメントでもない。
// 見出しの後ろにはコメントを書けるので（key: | # …）、見出しの判定からは、その行のコメントを外す。
func (s *lexer) blockScalar(header string, line int) {
	if n := len(s.toks); n > 0 {
		if t := s.toks[n-1]; t.Kind.IsComment() && t.Line == line && t.Col-1 <= len(header) {
			header = header[:t.Col-1]
		}
	}
	indent, ok := blockScalarHeader(header)
	if !ok {
		return
	}
	s.skipIndented(indent)
}

// skipIndented は、見出しより深く字下げされた行（と、その間の空行）を1つの文字列トークンとして
// 読み飛ばす。字下げが見出しまで戻った行で、ブロックスカラーは終わる。
func (s *lexer) skipIndented(parent string) {
	start, line, col := s.pos, s.line, s.col()
	end, endLine := s.pos, s.line

	for s.pos < len(s.src) {
		eol := s.pos
		for eol < len(s.src) && s.src[eol] != '\n' {
			eol++
		}
		text := string(s.src[s.pos:eol])
		if strings.TrimSpace(text) != "" && len(indentOf(text)) <= len(parent) {
			break
		}
		endLine = s.line
		s.pos = eol
		end = eol
		if s.pos < len(s.src) {
			s.newline()
		}
	}
	if end > start {
		s.emit(KindString, line, col, endLine, string(s.src[start:end]))
	}
}

// tryHeredoc は、現在位置がヒアドキュメントの開き（<<EOF / <<-'EOF'）なら、開きの記号だけを読み、
// 区切りを控える。中身が始まるのは次の行なので、ここでは読まない。区切り自身（EOF / 'EOF'）は
// 控えるだけで読まず、後ろの語や文字列として普通に読ませる。
func (s *lexer) tryHeredoc() bool {
	if !s.spec.Heredocs || !s.has("<<") || s.has("<<<") {
		return false
	}

	i := s.pos + 2
	dash := false
	if i < len(s.src) && s.src[i] == '-' {
		dash = true
		i++
	}

	j := i
	for j < len(s.src) && (s.src[j] == ' ' || s.src[j] == '\t') {
		j++
	}
	delim, ok := heredocDelim(s.src, j)
	if !ok {
		return false
	}

	line, col := s.line, s.col()
	text := string(s.src[s.pos:i])
	s.pos = i
	s.emit(KindPunct, line, col, line, text)
	s.pending = append(s.pending, heredoc{Delim: delim, Dash: dash})
	return true
}

// heredocDelim は、src の pos に立つヒアドキュメントの区切りを読む。引用符で囲めるが、囲まなければ
// 英字か「_」で始まる語でなければならない。数字や変数から始まるものを区切りと読むと、算術の左シフト
// （$(( x << 2 ))）がヒアドキュメントの開きに化ける。
func heredocDelim(src []byte, pos int) (string, bool) {
	if pos >= len(src) {
		return "", false
	}

	if q := src[pos]; q == '\'' || q == '"' {
		for i := pos + 1; i < len(src) && src[i] != '\n'; i++ {
			if src[i] == q {
				return string(src[pos+1 : i]), true
			}
		}
		return "", false
	}

	r, _ := utf8.DecodeRune(src[pos:])
	if r != '_' && !unicode.IsLetter(r) {
		return "", false
	}
	end := pos
	for end < len(src) {
		r, size := utf8.DecodeRune(src[end:])
		if !isWordRune(r) {
			break
		}
		end += size
	}
	return string(src[pos:end]), true
}

// heredocBody は、区切りだけの行までを1つの文字列トークンとして読み、その区切りの行ごと読み飛ばす。
// 中身はコメント記号を含みうるただの文字列であって、コードでもコメントでもない。
// 区切りが最後まで現れないなら、開きの読み違いなので、1バイトも読まずに戻る。読み違えたまま末尾まで
// 飲み込むと、その先にある本物のコメントが文字列の中に消え、検査されないまま通る。迷ったら違反にする
// 側へ倒す。
func (s *lexer) heredocBody(h heredoc) {
	end, next := s.findHeredocEnd(h)
	if end < 0 {
		return
	}

	start, line, col := s.pos, s.line, s.col()
	for s.pos < end {
		if s.src[s.pos] == '\n' {
			s.newline()
			continue
		}
		s.pos++
	}
	if end > start {
		s.emit(KindString, line, col, s.line-1, string(s.src[start:end]))
	}

	s.pos = next
	if s.pos < len(s.src) {
		s.newline()
	}
}

// findHeredocEnd は、区切りだけの行を探し、中身の終わり（区切りの行の頭）と、区切りの行の次の位置を
// 返す。見つからなければ end に -1 を返す。
func (s *lexer) findHeredocEnd(h heredoc) (end, next int) {
	for p := s.pos; p <= len(s.src); {
		eol := p
		for eol < len(s.src) && s.src[eol] != '\n' {
			eol++
		}

		text := string(s.src[p:eol])
		if h.Dash {
			text = strings.TrimLeft(text, "\t")
		}
		if strings.TrimRight(text, "\r") == h.Delim {
			return p, eol
		}
		if eol >= len(s.src) {
			return -1, 0
		}
		p = eol + 1
	}
	return -1, 0
}

func (s *lexer) lineComment(kind Kind) {
	start, line, col := s.pos, s.line, s.col()
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		s.pos++
	}
	text := strings.TrimSuffix(string(s.src[start:s.pos]), "\r")
	s.emit(kind, line, col, line, text)
}

func (s *lexer) blockComment(kind Kind) {
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

func (s *lexer) tryString() bool {
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
func (s *lexer) template(sp StringSpec, n int) {
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
func (s *lexer) interp() {
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
func (s *lexer) oneRune(sp StringSpec, n int, close string) bool {
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

// closes は、現在位置がその文字列を閉じるかを見る。ヒアストリングを閉じるのは行頭に立つ「"@」
// だけで、行の途中に現れたもの（$h = @{ k = "@x" }）では閉じない。
func (s *lexer) closes(sp StringSpec, close string) bool {
	if !s.has(close) {
		return false
	}
	return !sp.Here || s.pos == s.lineStart
}

// stringLit は、開きの長さ n と、それに対応する閉じ記号を受けて文字列リテラルを1つ読む
// （閉じ記号が開きに依るのは Rust の r#"…"# のような可変長の区切りがあるため）。改行を含められない
// 形が閉じずに行末に来たら、不正なソースなので、そこで打ち切る。
func (s *lexer) stringLit(sp StringSpec, n int, close string) {
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
		case s.closes(sp, close):
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
func (s *lexer) tryRegex() bool {
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
func (s *lexer) regexLit() bool {
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
func (s *lexer) exprCanStart() bool {
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

// lastCode は、直前のコードトークンを返す（無ければ nil）。
func (s *lexer) lastCode() *Token {
	for i := len(s.toks) - 1; i >= 0; i-- {
		if s.toks[i].Kind.IsCode() {
			return &s.toks[i]
		}
	}
	return nil
}

// mark はスキャナの状態の控え。読み直し（正規表現・JSX の当たりが外れたとき）に使う。
type mark struct {
	pos, line, lineStart, toks int
}

func (s *lexer) snapshot() mark {
	return mark{pos: s.pos, line: s.line, lineStart: s.lineStart, toks: len(s.toks)}
}

func (s *lexer) restore(m mark) {
	s.pos, s.line, s.lineStart = m.pos, m.line, m.lineStart
	s.toks = s.toks[:m.toks]
}

func (s *lexer) wordOrPunct() {
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
func (s *lexer) has(p string) bool {
	return hasAt(s.src, s.pos, p)
}

// hasDoc は、現在位置が doc 記法 p に当たるかを見る。記号を重ねた区切り線（//// … ）は
// doc ではないので、記法の最後の1字がそのまま続くなら当たったことにしない（当てると、
// 区切り線が doc の器を名乗り、そこが逃げ場になる）。
func (s *lexer) hasDoc(p string) bool {
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

func (s *lexer) col() int {
	return s.pos - s.lineStart + 1
}

func (s *lexer) newline() {
	s.pos++
	s.line++
	s.lineStart = s.pos
}

func (s *lexer) emit(kind Kind, line, col, endLine int, text string) {
	s.toks = append(s.toks, Token{Kind: kind, Line: line, Col: col, EndLine: endLine, Text: text})
}
