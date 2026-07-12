package rule

import (
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
	Syntax string

	// Path はツリーの根からの相対パス。区切りは「/」。
	Path string
}

// Lexicon は、コメントの本文をプロジェクトのルールに照らす（層2）。ツールは既定のルールを持たない
// ので、設定に rules: が無ければ何も起きない。違反のメッセージは各ルールが持つ（disposition は層1 の
// ためのもの）。ルールを並べた順に返す。呼ぶのは層1 を通ったコメントに対してだけで、置き場所や形が
// 違うコメントに語彙の違反まで重ねて出しても、ノイズにしかならない。当てる本文は折り返しを畳んだもの
// （scan.Unwrap）で、ルールの書き手は句が折り返される心配をせずに「no longer」と書ける。
func Lexicon(c place.Comment, rules []config.Rule, t Target, spec scan.LangSpec) []Violation {
	body := scan.Unwrap(scan.Body(c.Text, spec))

	var out []Violation
	for _, r := range rules {
		if !applies(r, c, t) {
			continue
		}
		if !r.Regexp.MatchString(body) {
			continue
		}
		v := violation(r.ID, c, nil)
		v.Message = r.Message
		out = append(out, *v)
	}
	return out
}

// applies は、ルールの where: がこのコメントに届くかを見る。省略した軸は絞らない。
func applies(r config.Rule, c place.Comment, t Target) bool {
	w := r.Where
	if len(w.Syntax) > 0 && !slices.Contains(w.Syntax, t.Syntax) {
		return false
	}
	if len(w.Kind) > 0 && !slices.Contains(w.KindValues(), c.Kind) {
		return false
	}
	if len(w.Path) > 0 && !glob.Selects(w.Path, t.Path) {
		return false
	}
	return true
}
