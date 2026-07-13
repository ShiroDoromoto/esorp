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
		fmt.Fprintf(&b, "%s:%d:%d  place=%s kind=%s\n", h.Path, h.Line, h.Col, h.Place, h.Kind)
		indent(&b, h.Text)
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "%s に %d 件が当たりました（%d ファイル / %d コメント中 %s）\n",
		t.Pattern, len(t.Hits), t.Files, t.Comments, share(len(t.Hits), t.Comments))
	b.WriteString("真陽性か偽陽性かは、esorp は判定しません。当たりを読んで、足すかどうかを決めてください。\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// share は、当たりが全コメントに占める割合。母集団が空なら書かない。
func share(hits, comments int) string {
	if comments == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.2f%%", float64(hits)/float64(comments)*100)
}

type jsonTrial struct {
	Version int            `json:"version"`
	Pattern string         `json:"pattern"`
	Summary jsonTrialSum   `json:"summary"`
	Hits    []jsonTrialHit `json:"hits"`
	Skipped []string       `json:"skipped"`
}

type jsonTrialSum struct {
	Files    int `json:"files"`
	Comments int `json:"comments"`
	Hits     int `json:"hits"`
}

// jsonTrialHit の body は照合に使った本文（折り返しを畳んだもの）。text（原文）だけでは、句が行を
// またいで当たったときに、なぜ当たったのかが読み取れない。
type jsonTrialHit struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Place string `json:"place"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Body  string `json:"body"`
}

// TryJSON は、候補パターンの当たりを機械可読で書く。
func TryJSON(w io.Writer, t *audit.Trial) error {
	out := jsonTrial{
		Version: 1,
		Pattern: t.Pattern,
		Summary: jsonTrialSum{Files: t.Files, Comments: t.Comments, Hits: len(t.Hits)},
		Hits:    make([]jsonTrialHit, 0, len(t.Hits)),
		Skipped: t.Skipped,
	}
	if out.Skipped == nil {
		out.Skipped = []string{}
	}
	for _, h := range t.Hits {
		out.Hits = append(out.Hits, jsonTrialHit{
			Path:  h.Path,
			Line:  h.Line,
			Col:   h.Col,
			Place: h.Place.String(),
			Kind:  h.Kind.String(),
			Text:  h.Text,
			Body:  h.Body,
		})
	}
	return encode(w, out)
}
