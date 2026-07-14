package audit

import (
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
// 返す違反はファイルのものと同じ形だが、パスも行も持たない（入力は本文1つで、その中の位置を esorp は
// 数えない）。器と種別は place.None / scan.KindNone——持っていないものを header や line と名乗らせない。
func Text(cfg *config.Config, body string) []rule.Violation {
	c := place.Comment{Text: body, Place: place.None, Kind: scan.KindNone}
	return rule.Lexicon(c, cfg.Rules, rule.Target{Syntax: config.SyntaxText}, scan.TextSpec())
}
