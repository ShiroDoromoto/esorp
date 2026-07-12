// Package glob は、パスを glob の並びに照らす。
//
// 設定でパスを絞る所は2つある（syntax.files: と rules[].where.path:）。どちらも「! 始まりで除外」
// と宣言しているので、照合をここ1つに置き、両方から引く。別々に書くと、同じ書き方が場所によって
// 違う意味を持ちうる（除外がいつも勝つのか、書いた順で勝つのか）。
package glob

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Selects は、glob の並びがそのパスを選ぶかを見る。「!」始まりは除外であり、正の glob に当たって
// いても除外に当たれば落とす（vendor/ や node_modules/ のように、自分のコードでないものを外す）。
// 除外がいつも勝つので、並べる順は結果を変えない。除外だけを並べた並びは、何も選ばない。
func Selects(globs []string, path string) bool {
	if Excluded(globs, path) {
		return false
	}
	for _, g := range globs {
		if !strings.HasPrefix(g, "!") && Match(g, path) {
			return true
		}
	}
	return false
}

// Excluded は、除外（! 始まり）のいずれかに当たるかを見る。
func Excluded(globs []string, path string) bool {
	for _, g := range globs {
		if pat, ok := strings.CutPrefix(g, "!"); ok && Match(pat, path) {
			return true
		}
	}
	return false
}

// Match は glob 1つの照合。** を含む照合は doublestar に任せる。glob が不正でないことは、設定を
// 読んだ時点で確かめてある。
func Match(glob, path string) bool {
	ok, err := doublestar.Match(glob, path)
	return err == nil && ok
}

// Valid は、glob として読める形かを見る。設定の検証が使う。不正な glob はどのパスにも当たらない
// ので、通してしまうと、そのファイル群は検査されないまま適合したように見える。
func Valid(glob string) bool {
	return doublestar.ValidatePattern(glob)
}
