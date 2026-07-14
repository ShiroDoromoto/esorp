package audit

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// Hit は、候補パターンが当たったコメント1つ。Body は照合に使った本文（折り返しを畳んだもの）で、
// Text は原文。原文だけでは、句が行をまたいで当たったときに、なぜ当たったのかが読み取れない。
// SeamDependent は、その当たりが折り返しの継ぎ目に左右されること（層2 が出す印と同じもの）。
type Hit struct {
	Path string

	// Syntax は、そのファイルを拾った syntax エントリの名前（where.syntax が照らすもの）。
	Syntax string

	Body          string
	SeamDependent bool
	place.Comment
}

// Trial は、候補パターンを自分のコーパスに当てた結果。esorp は真陽性と偽陽性を分けない——
// 当たりを読んで、その語彙を足すかどうかを決めるのは人間（あるいは層3 のエージェント）。
type Trial struct {
	Pattern  string
	Files    int
	Comments int
	Hits     []Hit
	Skipped  []string

	// Surfaces は、面（syntax エントリ）ごとの内訳。ある面のコーパスで誤検知ゼロだったパターンが、
	// 別の面では当たりまくる——それは普通に起きる（Go の doc と YAML のコメントでは、書かれる語彙が
	// そもそも違う）。全体の件数だけを見せると、その偏りが平均に埋もれる。
	Surfaces []Surface
}

// Surface は、面1つの内訳。名前順に並ぶ。
type Surface struct {
	Syntax   string
	Files    int
	Comments int
	Hits     int
}

// Try は、候補パターンをツリーの全コメントに当てる。層2 の語彙を足す前に、それが自分のコーパスで
// どれだけ誤検知するかを測る口（「稀なら足さない方がまし」と言うには、稀かどうかを測れなければ
// ならない）。当てる本文も当て方も層2 とまったく同じ（scan.Unwrap で畳んだ本文の、両方の読みに当て、
// どちらかで当たれば1件）なので、ここで当たった数は、その語彙を rules: に足したときに当たる数
// そのものになる。継ぎ目に左右される当たりも層2 と同じ印を持つ。ただし層1（器と書式）
// も where: の絞り（syntax / kind / path）も当てない。層2 が見るのは層1 を通ったコメントだけだが、
// それは違反を報告するときの順序であって、語彙の精度とは関係がない——導入前のツリーは層1 の違反を
// 大量に含んでいて、そこで母集団を絞ると、測りたいコーパスのほとんどが視界から消える。
func Try(cfg *config.Config, root string, re *regexp.Regexp) (*Trial, error) {
	t := &Trial{Pattern: re.String(), Hits: []Hit{}, Surfaces: []Surface{}}
	surfaces := map[string]*Surface{}

	paths, err := collect(cfg, root, nil)
	if err != nil {
		return nil, err
	}
	for _, m := range paths {
		comments, spec, ok, err := read(cfg, root, m)
		if err != nil {
			return nil, err
		}
		if !ok {
			t.Skipped = append(t.Skipped, m.path)
			continue
		}

		s, seen := surfaces[m.syntax]
		if !seen {
			s = &Surface{Syntax: m.syntax}
			surfaces[m.syntax] = s
		}
		t.Files++
		t.Comments += len(comments)
		s.Files++
		s.Comments += len(comments)

		for _, c := range comments {
			folded := scan.Unwrap(scan.BodyLines(c.Text, spec), spec)
			readings := folded.Readings()
			hits := 0
			for _, body := range readings {
				if re.MatchString(body) {
					hits++
				}
			}
			if hits == 0 {
				continue
			}
			s.Hits++
			t.Hits = append(t.Hits, Hit{
				Path:          m.path,
				Syntax:        m.syntax,
				Body:          folded.Text,
				SeamDependent: hits < len(readings),
				Comment:       c,
			})
		}
	}

	for _, name := range slices.Sorted(maps.Keys(surfaces)) {
		t.Surfaces = append(t.Surfaces, *surfaces[name])
	}

	slices.SortFunc(t.Hits, func(a, b Hit) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := a.Line - b.Line; c != 0 {
			return c
		}
		return a.Col - b.Col
	})
	return t, nil
}
