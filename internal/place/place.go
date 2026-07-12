// Package place は、コメントの位置クラスを判定する。
//
// ここが esorp の中核。コメントの中身は読まず、前後のトークンと、今どのスコープの中に
// いるかだけを見て、コメントが収まっている「器」を決める。判定は言語をまたいで同じで、
// 言語ごとに違うのは scan.LangSpec が持つ語彙だけ（字句と位置判定を混ぜない）。
package place

import (
	"slices"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// Place は位置クラス。
//
// 履歴・事情・作業メモが流れ込むのは Leading と Orphan であり、この2つを許可しなければ、
// 語彙を一切見ずに、それらは構造的に書けなくなる。
type Place int

const (
	Header   Place = iota // ファイル冒頭。前にコードトークンが1つも無い
	Doc                   // 直後が宣言で、間に空行が無く、宣言コンテキストにある
	Trailing              // 同じ行にコードがある（行末にぶら下がっている）
	Leading               // 直後がコードだが、宣言ではない（＝文の直前）
	Orphan                // 直後がコードでない（空行が挟まる / 閉じ括弧 / ファイル末尾）
)

func (p Place) String() string {
	switch p {
	case Header:
		return "header"
	case Doc:
		return "doc"
	case Trailing:
		return "trailing"
	case Leading:
		return "leading"
	case Orphan:
		return "orphan"
	default:
		return "unknown"
	}
}

// Comment は器1つ。連続する複数行コメントは1つの器として扱うので、
// 塊の先頭で位置クラスを判定し、塊全体に適用する。
type Comment struct {
	Kind    scan.Kind
	Place   Place
	Line    int
	Col     int
	EndLine int
	Text    string // 塊の生テキスト。複数トークンなら改行で連ねる
	Decl    string // 紐づく宣言の名前。Doc 以外、または取り出せないときは空
}

// opener は、そのスコープを開いたものの種類。
//
// doc を名乗れるのは openerFile（トップレベル）と openerTypeLike の中だけ。関数本体の中では
// 宣言の直前でも doc にならない。ここを緩めると、関数の中のローカル宣言の直前が履歴の逃げ場になる。
type opener int

const (
	openerFile opener = iota
	openerTypeLike
	openerFunc
	openerBlock
)

// Classify は、トークン列からコメントの器を取り出す。
func Classify(toks []scan.Token, spec scan.LangSpec) []Comment {
	var out []Comment
	scope := []opener{openerFile}

	for i := 0; i < len(toks); {
		if !toks[i].Kind.IsComment() {
			updateScope(&scope, toks, i, spec)
			i++
			continue
		}
		end := groupEnd(toks, i)
		out = append(out, classify(toks, i, end, scope[len(scope)-1], spec))
		i = end + 1
	}
	return out
}

// groupEnd は、i から始まるコメントの塊の最後のトークンの位置を返す。
// 塊になるのは、間にコードが無く、行が途切れずに続くコメントだけ。
// 行末コメントは、その行にコードがある以上いつも単独の器になる。
func groupEnd(toks []scan.Token, i int) int {
	if prev := prevCode(toks, i); prev != nil && prev.Line == toks[i].Line {
		return i
	}
	end := i
	for j := i + 1; j < len(toks) && toks[j].Kind.IsComment(); j++ {
		if toks[j].Line != toks[end].EndLine+1 {
			break // 空行が挟まれば、そこから先は別の器
		}
		end = j
	}
	return end
}

// classify は、判定表を上から順に当てる（最初に当たったものを採る）。
func classify(toks []scan.Token, start, end int, scope opener, spec scan.LangSpec) Comment {
	c := Comment{
		Kind:    toks[start].Kind,
		Line:    toks[start].Line,
		Col:     toks[start].Col,
		EndLine: toks[end].EndLine,
		Text:    groupText(toks, start, end),
	}

	prev := prevCode(toks, start)
	next, nextIdx := nextCode(toks, end)

	switch {
	case c.Kind == scan.KindDocLine || c.Kind == scan.KindDocBlock:
		// doc 専用記法は字句だけで doc と分かる。位置判定より kind を優先する
		// （Rust の //! はファイル冒頭にあっても header ではなく doc）。
		c.Place = Doc
	case prev == nil:
		c.Place = Header
	case prev.Line == c.Line:
		c.Place = Trailing
	case next == nil || isCloser(*next):
		c.Place = Orphan
	case next.Line > c.EndLine+1:
		c.Place = Orphan
	case startsDecl(*next, scope, spec):
		c.Place = Doc
	default:
		c.Place = Leading
	}

	// 名前を取り出せるのは、直後が本当に宣言のときだけ。doc 記法は関数の中でも doc になるが、
	// そこに紐づく宣言は無い。
	if c.Place == Doc && next != nil && startsDecl(*next, scope, spec) {
		c.Decl = declName(toks, nextIdx, spec)
	}
	return c
}

// updateScope は、i のトークンが括弧なら、スコープスタックを開閉する。
func updateScope(scope *[]opener, toks []scan.Token, i int, spec scan.LangSpec) {
	t := toks[i]
	if t.Kind != scan.KindPunct {
		return
	}
	switch t.Text {
	case "{":
		*scope = append(*scope, openerOf(toks, i, spec))
	case "}":
		if len(*scope) > 1 { // openerFile は積んだままにする
			*scope = (*scope)[:len(*scope)-1]
		}
	}
}

// openerOf は、i の「{」が何を開いたのかを、その行の「{」より前のキーワードから決める。
// 関数リテラル（go func() { … }）も、行のどこかに func があるので func として拾える。
func openerOf(toks []scan.Token, i int, spec scan.LangSpec) opener {
	found := openerBlock
	for j := i - 1; j >= 0 && toks[j].Line == toks[i].Line; j-- {
		if toks[j].Kind != scan.KindWord {
			continue
		}
		if slices.Contains(spec.FuncOpeners, toks[j].Text) {
			return openerFunc
		}
		if slices.Contains(spec.TypeLikeOpeners, toks[j].Text) {
			found = openerTypeLike
		}
	}
	return found
}

// startsDecl は、next のトークンが宣言の始まりであることを、今いるスコープに照らして見る。
//
// トップレベルでは、宣言はキーワードで始まる。型を定義するブロックの中は、フィールドも
// メソッドもすべて宣言なので、キーワードが無くても宣言として扱う（struct のフィールドや
// interface のメソッドに付いた doc を落とさないため）。
// 関数本体とただのブロックの中に、doc を名乗れる宣言は無い。
func startsDecl(next scan.Token, scope opener, spec scan.LangSpec) bool {
	switch scope {
	case openerFile:
		return isDecl(next, spec)
	case openerTypeLike:
		return next.Kind == scan.KindWord
	default:
		return false
	}
}

// declName は、コメントの直後の宣言から名前を取り出す。書式の subject がこれを使う。
func declName(toks []scan.Token, i int, spec scan.LangSpec) string {
	if i < 0 || i >= len(toks) || toks[i].Kind != scan.KindWord {
		return ""
	}
	// 型の中のフィールドのように、キーワードを伴わない宣言は、その語自体が名前。
	if !isDecl(toks[i], spec) {
		return toks[i].Text
	}

	kw := toks[i].Text
	i++
	// 宣言キーワードが続くことがある（Rust の pub fn、TS の export function）。最後のものが本体。
	for i < len(toks) && isDecl(toks[i], spec) {
		kw = toks[i].Text
		i++
	}
	// Go のメソッドは名前の前にレシーバを挟む: func (s *Scanner) Scan(…)
	if slices.Contains(spec.FuncOpeners, kw) && i < len(toks) && toks[i].Kind == scan.KindPunct && toks[i].Text == "(" {
		i = skipParens(toks, i)
	}
	if i < len(toks) && toks[i].Kind == scan.KindWord {
		return toks[i].Text
	}
	// var ( … ) のような括弧でまとめた宣言には、紐づく名前が無い。
	return ""
}

// skipParens は、i の「(」に対応する「)」の次の位置を返す。
func skipParens(toks []scan.Token, i int) int {
	depth := 0
	for ; i < len(toks); i++ {
		if toks[i].Kind != scan.KindPunct {
			continue
		}
		switch toks[i].Text {
		case "(":
			depth++
		case ")":
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return i
}

func prevCode(toks []scan.Token, i int) *scan.Token {
	for j := i - 1; j >= 0; j-- {
		if !toks[j].Kind.IsComment() {
			return &toks[j]
		}
	}
	return nil
}

func nextCode(toks []scan.Token, i int) (*scan.Token, int) {
	for j := i + 1; j < len(toks); j++ {
		if !toks[j].Kind.IsComment() {
			return &toks[j], j
		}
	}
	return nil, -1
}

func isDecl(t scan.Token, spec scan.LangSpec) bool {
	return t.Kind == scan.KindWord && slices.Contains(spec.DeclKeywords, t.Text)
}

func isCloser(t scan.Token) bool {
	return t.Kind == scan.KindPunct && (t.Text == "}" || t.Text == ")" || t.Text == "]")
}

func groupText(toks []scan.Token, start, end int) string {
	var b strings.Builder
	for i := start; i <= end; i++ {
		if i > start {
			b.WriteByte('\n')
		}
		b.WriteString(toks[i].Text)
	}
	return b.String()
}
