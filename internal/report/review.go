package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/audit"
)

// ReviewJSON は、層3 に渡す材料を機械可読で書く。読むのはエージェントなので、これが既定の形。
// check --diff --format json の review と同じ形にしてあるので、答える側は入口を1つ覚えれば足りる。
func ReviewJSON(w io.Writer, rv *audit.Review) error {
	out := jsonReviewDoc{
		Version:  1,
		Question: strings.TrimRight(rv.Question, "\n"),
		Summary:  jsonReviewSum{Comments: len(rv.Comments)},
		Comments: make([]jsonComment, 0, len(rv.Comments)),
	}
	for _, c := range rv.Comments {
		out.Comments = append(out.Comments, jsonComment{
			Path:  c.Path,
			Line:  c.Line,
			Col:   c.Col,
			Place: c.Place.String(),
			Kind:  c.Kind.String(),
			Text:  c.Text,
		})
	}
	return encode(w, out)
}

type jsonReviewDoc struct {
	Version  int           `json:"version"`
	Question string        `json:"question"`
	Summary  jsonReviewSum `json:"summary"`
	Comments []jsonComment `json:"comments"`
}

type jsonReviewSum struct {
	Comments int `json:"comments"`
}

// ReviewText は、層3 に渡す材料を人間向けに書く。件数と、渡す先が誰かを言うだけ——判定を書かない
// のは、esorp が判定しないため。全部出す（サンプリングしない）。多すぎて読めないなら、パスで
// 絞ってください。
func ReviewText(w io.Writer, rv *audit.Review) error {
	var b strings.Builder
	for _, c := range rv.Comments {
		fmt.Fprintf(&b, "%s:%d:%d  place=%s kind=%s\n", c.Path, c.Line, c.Col, c.Place, c.Kind)
		indent(&b, c.Text)
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "%d comments passed layers 1 and 2.\n\n", len(rv.Comments))
	indent(&b, strings.TrimRight(rv.Question, "\n"))
	b.WriteString("\nThe one who answers is not esorp — it is the agent reading this output.\n")

	_, err := io.WriteString(w, b.String())
	return err
}
