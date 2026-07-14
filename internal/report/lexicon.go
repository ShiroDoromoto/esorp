package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/audit"
)

// TryText は、候補パターンの当たりを人間向けに書く。当たりは全部出す（サンプリングしない）——
// 何件かを黙って隠せば、測った数が信用できなくなる。多すぎて読めないなら、それ自体が「この語彙は
// 広すぎる」という答え。
func TryText(w io.Writer, t *audit.Trial) error {
	var b strings.Builder
	for _, h := range t.Hits {
		fmt.Fprintf(&b, "%s:%d:%d  place=%s kind=%s%s\n", h.Path, h.Line, h.Col, h.Place, h.Kind, seamMark(h.SeamDependent))
		indent(&b, h.Text)
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "%s に %d 件が当たりました（%d ファイル / %d コメント中 %s）\n",
		t.Pattern, len(t.Hits), t.Files, t.Comments, share(len(t.Hits), t.Comments))

	if len(t.Surfaces) > 0 {
		b.WriteString("\n面ごとの内訳:\n")
		for _, s := range t.Surfaces {
			fmt.Fprintf(&b, "  %-16s %4d 件 / %d コメント（%s）\n",
				s.Syntax, s.Hits, s.Comments, share(s.Hits, s.Comments))
		}
	}

	fmt.Fprintf(&b, "\n%s\n", TrySurfaceNote)
	b.WriteString("真陽性か偽陽性かは、esorp は判定しません。当たりを読んで、足すかどうかを決めてください。\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// TrySurfaceNote は、面ごとの内訳に添える断り。ある面のコーパスで誤検知ゼロだったパターンが、別の面
// では当たりまくる——それは普通に起きるので、面をまたいで見る。text 面（check --text に渡される
// 本文）だけは測れない。コミットメッセージは手元のツリーに残っておらず、当てるコーパスが無い——
// 「測ってから足す」は、この面には効かせようがない。無いものを 0 件と書けば、測ったように見える。
const TrySurfaceNote = `text 面（check --text）は測れません。渡される本文はツリーの外にあり、当てるコーパスがありません。
この面に当てるルールは、当たりを見て決めてください（0 件と出しているのではなく、測っていません）。`

// seamMark は、折り返しの継ぎ目に左右される当たりに添える印（→ SeamNote）。当たりの見出しに出す
// のは、その1件だけの話だから——測っている最中に、どの当たりが継ぎ目のせいかを見分けられる。
func seamMark(dependent bool) string {
	if !dependent {
		return ""
	}
	return "  seam=dependent"
}

// share は、当たりが全コメントに占める割合。母集団が空なら書かない。
func share(hits, comments int) string {
	if comments == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.2f%%", float64(hits)/float64(comments)*100)
}

// jsonTrial の text_surface は、text 面が測れないこと（→ TrySurfaceNote）。measured を false で
// 立てるのは、0 件と読まれないため——測っていないことと、測って当たらなかったことは違う。
type jsonTrial struct {
	Version     int             `json:"version"`
	Pattern     string          `json:"pattern"`
	Summary     jsonTrialSum    `json:"summary"`
	Surfaces    []jsonTrialFace `json:"surfaces"`
	TextSurface jsonTextSurface `json:"text_surface"`
	Hits        []jsonTrialHit  `json:"hits"`
	Skipped     []string        `json:"skipped"`
}

type jsonTrialSum struct {
	Files    int `json:"files"`
	Comments int `json:"comments"`
	Hits     int `json:"hits"`
}

// jsonTrialFace は、面1つの内訳。
type jsonTrialFace struct {
	Syntax   string `json:"syntax"`
	Files    int    `json:"files"`
	Comments int    `json:"comments"`
	Hits     int    `json:"hits"`
}

type jsonTextSurface struct {
	Measured bool   `json:"measured"`
	Reason   string `json:"reason"`
}

// jsonTrialHit の body は照合に使った本文（折り返しを畳んだもの）。text（原文）だけでは、句が行を
// またいで当たったときに、なぜ当たったのかが読み取れない。seam_dependent は、その当たりが折り返しの
// 継ぎ目に左右されること（立ったときだけ出す）。
type jsonTrialHit struct {
	Path          string `json:"path"`
	Syntax        string `json:"syntax"`
	Line          int    `json:"line"`
	Col           int    `json:"col"`
	Place         string `json:"place"`
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	Body          string `json:"body"`
	SeamDependent bool   `json:"seam_dependent,omitempty"`
}

// TryJSON は、候補パターンの当たりを機械可読で書く。
func TryJSON(w io.Writer, t *audit.Trial) error {
	out := jsonTrial{
		Version:     2,
		Pattern:     t.Pattern,
		Summary:     jsonTrialSum{Files: t.Files, Comments: t.Comments, Hits: len(t.Hits)},
		Surfaces:    make([]jsonTrialFace, 0, len(t.Surfaces)),
		TextSurface: jsonTextSurface{Measured: false, Reason: TrySurfaceNote},
		Hits:        make([]jsonTrialHit, 0, len(t.Hits)),
		Skipped:     t.Skipped,
	}
	if out.Skipped == nil {
		out.Skipped = []string{}
	}
	for _, s := range t.Surfaces {
		out.Surfaces = append(out.Surfaces, jsonTrialFace{
			Syntax:   s.Syntax,
			Files:    s.Files,
			Comments: s.Comments,
			Hits:     s.Hits,
		})
	}
	for _, h := range t.Hits {
		out.Hits = append(out.Hits, jsonTrialHit{
			Path:          h.Path,
			Syntax:        h.Syntax,
			Line:          h.Line,
			Col:           h.Col,
			Place:         h.Place.String(),
			Kind:          h.Kind.String(),
			Text:          h.Text,
			Body:          h.Body,
			SeamDependent: h.SeamDependent,
		})
	}
	return encode(w, out)
}
