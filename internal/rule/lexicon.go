package rule

import (
	"fmt"
	"slices"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/glob"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// Target は、語彙のルールを絞る軸のうち、コメント自身から取れないもの。where.syntax は設定の
// どのエントリが拾ったか、where.path はどのファイルかであり、どちらも走査の側が知っている。
type Target struct {
	// Syntax は、そのファイルを拾った syntax エントリの名前（where.syntax が照らすもの）。
	// 取り出しの無い入力（素の本文）は config.SyntaxText。
	Syntax string

	// Path はツリーの根からの相対パス。区切りは「/」。取り出しの無い入力には無い（空）。
	Path string
}

// Lexicon は、コメントの本文をプロジェクトのルールに照らす（層2）。ツールは既定のルールを持たない
// ので、設定に rules: が無ければ何も起きない。違反のメッセージは各ルールが持つ（disposition は層1 の
// ためのもの）。ルールを並べた順に返す。呼ぶのは層1 を通ったコメントに対してだけで、置き場所や形が
// 違うコメントに語彙の違反まで重ねて出しても、ノイズにしかならない。当てる本文は折り返しを畳んだもの
// （scan.Unwrap）で、ルールの書き手は句が折り返される心配をせずに二語の句を書ける。畳むのは散文だけ
// なので、doc のコードブロックの中で当たることはあっても、その行をまたぐことはない。半角と全角の
// 境目の継ぎ目だけは原文の空白を復元できず、畳んだ本文が2通りになる（scan.Folded.Readings）ので、
// どちらかで当たれば違反とする——通した違反は誰も拾わないが、検知は人の目に触れる。ただし片方でしか
// 当たらない当たりは継ぎ目が作った可能性があるので、印を付けて報告する（SeamDependent）。検知に
// 寄せたうえで、黙って誤爆させない。
func Lexicon(c place.Comment, rules []config.Rule, t Target, spec scan.LangSpec) []Violation {
	readings := scan.Unwrap(scan.BodyLines(c.Text, spec), spec).Readings()

	var out []Violation
	for i, r := range rules {
		if !applies(r, c, t) {
			continue
		}
		hits := 0
		for _, body := range readings {
			if r.Regexp.MatchString(body) {
				hits++
			}
		}
		if hits == 0 {
			continue
		}
		site := Site{Path: fmt.Sprintf("rules[%d]", i), Allow: -1, Rule: i}
		v := violation(r.ID, site, c, nil)
		v.Message = r.Message
		v.SeamDependent = hits < len(readings)
		out = append(out, *v)
	}
	return out
}

// applies は、ルールの where: がこのコメントに届くかを見る。省略した軸は絞らない（where.syntax を
// 省いたルールは、取り出しの無い入力にも当たる——共有が既定で、例外だけ宣言する）。
// 取り出しの無い入力を絞れる軸は syntax だけで、kind（コメントの種別）も path（ファイル）も、
// その入力には存在しない。無い軸で絞ったルールは当たらない——持っていない性質に対して絞りを
// 黙って無視すると、設定に書いた絞りが効かないモードが生まれる。
func applies(r config.Rule, c place.Comment, t Target) bool {
	w := r.Where
	if len(w.Syntax) > 0 && !slices.Contains(w.Syntax, t.Syntax) {
		return false
	}
	text := t.Syntax == config.SyntaxText
	if len(w.Kind) > 0 && (text || !slices.Contains(w.KindValues(), c.Kind)) {
		return false
	}
	if len(w.Path) > 0 && (text || !glob.Selects(w.Path, t.Path)) {
		return false
	}
	return true
}
