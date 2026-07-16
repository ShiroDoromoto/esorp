package audit

import (
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// Text は、取り出しの要らない入力——渡された文字列そのものが本文——に層2（語彙）だけを当てる。
// コメントから追い出した事情の逃げ場（コミットメッセージ・PR 本文・リリースノート）にも同じ語彙を
// 当てられるようにするための口で、禁止語彙の源泉は esorp.yaml 1つに保たれる。何を流し込むかは
// 呼び手の仕事であり、この口が git を知ることはない。層1（器・書式）は当てない——器はコメントが
// コードのどこに置かれているかの話であり、素のテキストは器を持たない。「層1 を切るモード」ではなく、
// 「取り出しの要らない入力には層2 だけが当たる」であって、何が当たるかは入力の構造から導かれる。
// 面は where.syntax の予約値 text（config.SyntaxText）で、where.syntax を省いたルールも当たる。
// 当てるのは段落ごとで、違反は入力の中の行を持つ（ファイルのようなパスは持たない）。器と種別は
// place.None / scan.KindNone——持っていないものを header や line と名乗らせない。
func Text(cfg *config.Config, body string) []rule.Violation {
	spec := scan.TextSpec()
	target := rule.Target{Syntax: config.SyntaxText}

	out := []rule.Violation{}
	for _, p := range paragraphs(body) {
		c := place.Comment{Text: p.text, Line: p.line, EndLine: p.endLine, Place: place.None, Kind: scan.KindNone}
		out = append(out, stamp(rule.Lexicon(c, cfg.Rules, target, spec), cfg.Severity)...)
	}
	return out
}

// paragraph は、本文の段落1つと、それが入力の何行目から始まるか。
type paragraph struct {
	text    string
	line    int
	endLine int
}

// paragraphs は、本文を空行で段落に割る。段落ごとに当てるのは、違反の位置を入力の行で示すため
// ——本文を丸ごと1つの塊として当てると、どこを直せばよいのかを書き手に返せない。折り返しを畳む
// 単位も段落なので（scan.Unwrap は空行を段落の区切りとして残す）、割っても当たり方は変わらない。
func paragraphs(body string) []paragraph {
	var out []paragraph

	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		blank := strings.TrimSpace(line) == ""
		switch {
		case blank && start >= 0:
			out = append(out, paragraph{
				text:    strings.Join(lines[start:i], "\n"),
				line:    start + 1,
				endLine: i,
			})
			start = -1
		case !blank && start < 0:
			start = i
		}
	}
	if start >= 0 {
		out = append(out, paragraph{
			text:    strings.Join(lines[start:], "\n"),
			line:    start + 1,
			endLine: len(lines),
		})
	}
	return out
}
