package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/rule"
)

// BodyText は、取り出しの要らない入力（check --text -）の違反を人間向けに書く。入力は本文1つで、
// パスも行も持たないので、違反 id と当たった本文と始末のしかたを並べる。
//
//	no-history
//	  認証を直す
//	  この関数の同期版は削除ずみ。
//	  変化を語っています。今の姿だけを書いてください。
func BodyText(w io.Writer, vs []rule.Violation) error {
	if len(vs) == 0 {
		_, err := io.WriteString(w, "esorp: 違反はありません\n")
		return err
	}

	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "%s\n", v.ID)
		indent(&b, v.Text)
		indent(&b, v.Message)
		if v.SeamDependent {
			indent(&b, SeamNote)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%d 件の違反\n", len(vs))

	_, err := io.WriteString(w, b.String())
	return err
}

// jsonBodyReport は、取り出しの要らない入力の機械可読出力。ファイルの監査（jsonReport）とは別の形に
// してある——パスも行も、検査したファイル数も、この入力には無い。無い欄を null で埋めて同じ形に
// 見せると、読み手はそこに値が来ることを期待する。
type jsonBodyReport struct {
	Version    int                 `json:"version"`
	Summary    jsonBodySummary     `json:"summary"`
	Violations []jsonBodyViolation `json:"violations"`
}

type jsonBodySummary struct {
	Violations int `json:"violations"`
}

type jsonBodyViolation struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	Message       string `json:"message"`
	SeamDependent bool   `json:"seam_dependent,omitempty"`
}

// BodyJSON は、取り出しの要らない入力の違反を機械可読で書く。violations は、空でも null でなく空配列。
func BodyJSON(w io.Writer, vs []rule.Violation) error {
	out := jsonBodyReport{
		Version:    1,
		Summary:    jsonBodySummary{Violations: len(vs)},
		Violations: make([]jsonBodyViolation, 0, len(vs)),
	}
	for _, v := range vs {
		out.Violations = append(out.Violations, jsonBodyViolation{
			ID:            v.ID,
			Text:          v.Text,
			Message:       strings.TrimRight(v.Message, "\n"),
			SeamDependent: v.SeamDependent,
		})
	}
	return encode(w, out)
}
