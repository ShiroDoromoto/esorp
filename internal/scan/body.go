package scan

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Body は、コメントの生テキストから記号を剥がして本文だけにする。剥がすのは、行コメント・ブロック
// コメント・doc 記法の記号と、継ぎ行に添えられる「*」（それを添える流儀 spec.BlockStars のときだけ。
// <!-- --> で剥がすと箇条書きの「*」が消える）、そして前後の空白と、
// 記号だけになって残る前後の行（中の空行は段落の区切りなので残す）。本文そのものには手を触れない。
// ラベルの判定（rule）と baseline のキー計算（baseline）が、これを共通の入口にする。
func Body(text string, spec LangSpec) string {
	openers := commentOpeners(spec)

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if c := blockClose(spec, line); c != "" {
			line = strings.TrimSpace(strings.TrimSuffix(line, c))
		}
		line = trimLongestPrefix(line, openers)
		if rest, ok := strings.CutPrefix(line, "*"); ok && spec.BlockStars {
			line = rest
		}
		lines = append(lines, strings.TrimSpace(line))
	}

	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// BodyLines は、コメント記号を剥がし、記号の内側の字下げを残した本文の行を返す。字下げを落とす
// Body では、doc のコードブロックと散文を見分けられない（→ CodeLines）。行コメントは記号（//）が
// 字下げの外側にあるので、内側と外側は記号で分かれるが、記号を持たないブロックコメントの継ぎ行では
// 分かれ目が無い。そこで、継ぎ行に共通する字下げ（commonIndent）を外側とし、そこを超える分だけを
// 内側として残す。字下げは相対でしか意味を持たない（go/doc も同じ形で剥がす）。全部剥がすと、タブで
// 字下げしたコードブロックが散文に化け、層1 では段落として数えられ、層2 では前後の行と畳まれる。
func BodyLines(text string, spec LangSpec) []string {
	openers := commentOpeners(spec)

	raw := strings.Split(text, "\n")
	for i, s := range raw {
		t := strings.TrimRight(s, " \t")
		if c := blockClose(spec, t); c != "" {
			raw[i] = strings.TrimSuffix(t, c)
		}
	}
	outer := ""
	if len(raw) > 1 {
		outer = commonIndent(raw[1:])
	}

	var lines []string
	for i, s := range raw {
		if i > 0 {
			s = strings.TrimPrefix(s, outer)
		}
		if o := longestPrefix(s, openers); o != "" {
			s = strings.TrimPrefix(s, o)
			if spec.BlockStars {
				s = strings.TrimPrefix(s, "*")
			}
		} else if spec.BlockStars {
			s = trimContinuationStar(s)
		}
		s = strings.TrimPrefix(s, " ")
		lines = append(lines, strings.TrimRight(s, " \t"))
	}

	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Folded は、折り返しを畳んだ本文と、そのうち賭けで空白を挟んだ継ぎ目。
type Folded struct {
	// Text は、継ぎ目に空白を挟んだ読み。
	Text string

	// Uncertain は、原文に空白が在ったかを復元できない継ぎ目の位置（Text の中の、挟んだ空白の
	// byte offset）。半角と全角の境目がこれになる。
	Uncertain []int
}

// Readings は、照合に当てる本文を返す。不確かな継ぎ目が無ければ1つ。在れば、そこに空白を挟んだ読みと、
// 挟まない読みの2つ。どちらが原文だったかは折り返した時点で失われているので、当てる側は両方を見て、
// 片方でしか当たらない当たりを「継ぎ目に依存する」と告げられる。
func (f Folded) Readings() []string {
	if len(f.Uncertain) == 0 {
		return []string{f.Text}
	}

	var b strings.Builder
	b.Grow(len(f.Text))
	prev := 0
	for _, at := range f.Uncertain {
		b.WriteString(f.Text[prev:at])
		prev = at + 1
	}
	b.WriteString(f.Text[prev:])
	return []string{f.Text, b.String()}
}

// Unwrap は、折り返しで途切れた本文を、段落ごとに1行へ畳む。語彙のルール（層2）は正規表現を本文に
// 当てるので、畳まないと二語の句が「no\nlonger」のように途切れて当たらず、検査したのに通ったように
// 見える。継ぎ目に空白を挟むかは両側の文字で決め、両側とも東アジアの全角なら挟まない（日本語の
// 折り返しは空白を伴わないので、「かつ\nて」は元の1語に戻る）。空行は段落の区切りとして残り、
// 畳んだ段落を隔てる改行になる。受けるのは字下げを残した行（BodyLines）で、畳むのは散文だけ。doc の
// コードブロックの行（CodeLines）はそのまま置き、前後の散文とも地続きにしない。コードは折り返された
// 散文ではないので、つないでも意味のある文にはならず、ルールがコードの行をまたいで当たるだけになる。
// 半角と全角の境目の継ぎ目だけは、空白を挟むかが賭けになる（「`--diff`」「を見る」は原文に空白が
// 無く、「script は」「Common」には在る）。畳んだ本文は挟んだ側に倒したうえで、その位置を Uncertain
// に残し、挟まない読みも取れるようにする。呼ぶのは照合の直前だけで、baseline のキーが見る本文は
// Body のままなので、既存のキーは動かない。
func Unwrap(lines []string, spec LangSpec) Folded {
	code := CodeLines(lines, spec)

	var out strings.Builder
	var uncertain []int
	open := false

	sep := func() {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
	}

	for i, line := range lines {
		switch {
		case line == "":
			open = false
		case code[i]:
			open = false
			sep()
			out.WriteString(line)
		case !open:
			sep()
			out.WriteString(line)
			open = true
		default:
			prev, next := lastRune(out.String()), firstRune(line)
			if needsSpace(prev, next) {
				if isWide(prev) != isWide(next) {
					uncertain = append(uncertain, out.Len())
				}
				out.WriteByte(' ')
			}
			out.WriteString(line)
		}
	}
	return Folded{Text: out.String(), Uncertain: uncertain}
}

// needsSpace は、折り返しの継ぎ目に空白を挟むかを返す。両側とも全角のときだけ挟まない。
func needsSpace(prev, next rune) bool {
	return !(isWide(prev) && isWide(next))
}

// isWide は、折り返しの継ぎ目に空白を伴わない文字（東アジアの全角）であることを表す。漢字・かな・
// ハングルは Unicode の script が持っているので、そのまま借りる。
func isWide(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, wideSymbols)
}

// wideSymbols は、script では拾えない全角の記号（約物「、。」、長音「ー」、全角形）。これらの script
// は Common であり、ラテン文字と同じ扱いになってしまう。
var wideSymbols = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x3000, Hi: 0x303F, Stride: 1},
		{Lo: 0x30FB, Hi: 0x30FC, Stride: 1},
		{Lo: 0xFF00, Hi: 0xFF60, Stride: 1},
		{Lo: 0xFFE0, Hi: 0xFFE6, Stride: 1},
	},
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// CodeLines は、doc の中のコードブロックの行に印を付ける。印の付いた行は、層1 では段落に数えず、
// 層2 では畳まない。行だけを見て分かるのはタブの字下げまでで、フェンス（```）は開きと閉じの間という
// 状態を持つので、本文を行の並びで見る。フェンスの行そのものも散文ではないので、コードブロックに
// 含める。閉じないまま終わったフェンスは、本文の終わりまでをコードブロックとする（Markdown と同じ）。
// フェンスを器と認めるのは、doc が Markdown の言語（spec.DocFences）だけ。Go の doc は Markdown では
// なく、コードブロックはタブの字下げで書くので、Go でフェンスを認めても誤爆は1つも減らず、背景を
// 囲って隠す逃げ場が増えるだけになる。
func CodeLines(lines []string, spec LangSpec) []bool {
	code := make([]bool, len(lines))
	inFence := false

	for i, line := range lines {
		switch {
		case spec.DocFences && isFence(line):
			code[i] = true
			inFence = !inFence
		case inFence:
			code[i] = true
		default:
			code[i] = strings.HasPrefix(line, "\t")
		}
	}
	return code
}

// isFence は、コードブロックを開閉するフェンスの行であることを表す。見分けるのはバッククォート3つ
// だけで、チルダ（~~~）は見ない。Rust / TypeScript の doc がコード例に使うのはバッククォートであり、
// 認める形を増やすほど、散文を囲って隠せる形が増える。
func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
}

// commentOpeners は、剥がすべきコメント記号を返す（当てる longestPrefix が長い順に選ぶ）。
// /// は // を接頭辞に含む。字句が複数の記号を持つ（設定の comments:）なら、そのすべてを並べる。
func commentOpeners(spec LangSpec) []string {
	openers := make([]string, 0, len(spec.DocLine)+len(spec.DocBlock)+2)
	openers = append(openers, spec.DocLine...)
	openers = append(openers, spec.DocBlock...)
	openers = append(openers, spec.lineMarkers()...)
	for _, p := range spec.blockPairs() {
		if p[0] != "" {
			openers = append(openers, p[0])
		}
	}
	return openers
}

// blockClose は、s の末尾に当たるブロックコメントの閉じ記号のうち、いちばん長いものを返す（無ければ
// 空）。字句は複数の対を持てる（設定の comments.block）ので、対ごとの閉じを見比べる。
func blockClose(spec LangSpec, s string) string {
	best := ""
	for _, p := range spec.blockPairs() {
		if c := p[1]; c != "" && strings.HasSuffix(s, c) && len(c) > len(best) {
			best = c
		}
	}
	return best
}

func trimLongestPrefix(s string, prefixes []string) string {
	return strings.TrimPrefix(s, longestPrefix(s, prefixes))
}

// longestPrefix は、s の先頭に当たるいちばん長い記号を返す。当たらなければ空。
func longestPrefix(s string, prefixes []string) string {
	best := ""
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) && len(p) > len(best) {
			best = p
		}
	}
	return best
}

// commonIndent は、行に共通する行頭の空白・タブのうち、いちばん長いものを返す。空白しか無い行は
// 字下げを持たないので数えない（コメントを閉じる「*/」だけの行と、段落を隔てる空行がこれになる）。
func commonIndent(lines []string) string {
	common := ""
	first := true
	for _, s := range lines {
		body := strings.TrimLeft(s, " \t")
		if body == "" {
			continue
		}
		indent := s[:len(s)-len(body)]
		if first {
			common, first = indent, false
			continue
		}
		n := 0
		for n < len(common) && n < len(indent) && common[n] == indent[n] {
			n++
		}
		common = common[:n]
	}
	return common
}

// trimContinuationStar は、ブロックコメントの継ぎ行に添えた「*」を、その手前の空白ごと落とす。
// 飛ばすのが空白だけなのは、コードブロックの字下げがタブだから。タブの先にある「*p = 1」のような
// 行を、継ぎ記号と読み違えない。
func trimContinuationStar(s string) string {
	t := strings.TrimLeft(s, " ")
	if rest, ok := strings.CutPrefix(t, "*"); ok {
		return rest
	}
	return s
}
