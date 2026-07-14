package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/rule"
)

// BodyNote は、取り出しの要らない入力（check --text）に何が当たり、何が当たらないかの断り。当たる
// のは層2（語彙）だけで、層1（器・書式）は当たらない——渡された本文は、コードのどこに置かれたもの
// でもないので、器を持たない。baseline も無い——抑制のキーはパスと本文で立つので、その場限りの入力
// には成立しない。黙って飛ばすと、通ったことが「層1 も通った」「baseline で抑えられる」と読まれる。
// 当たらない層は、出力で言う。
const BodyNote = `当たったのは層2（語彙）だけです。層1（器・書式）は当たりません（渡された本文は器を持ちません）。
baseline はありません（その場限りの入力なので、抑制のキーが立ちません）。`

// BodyText は、取り出しの要らない入力（check --text）の違反を人間向けに書く。位置は入力の中の行
// （パスは無い）。当たった段落と始末のしかたを添える。
//
//	3  no-history
//	  この関数の同期版は削除ずみ。
//	  変化を語っています。今の姿だけを書いてください。
func BodyText(w io.Writer, vs []rule.Violation) error {
	var b strings.Builder

	for _, v := range vs {
		fmt.Fprintf(&b, "%d  %s\n", v.Line, v.ID)
		indent(&b, v.Text)
		indent(&b, v.Message)
		if v.SeamDependent {
			indent(&b, SeamNote)
		}
		b.WriteByte('\n')
	}

	if len(vs) == 0 {
		b.WriteString("esorp: 違反はありません\n")
	} else {
		fmt.Fprintf(&b, "%d 件の違反\n", len(vs))
	}
	fmt.Fprintf(&b, "%s\n", BodyNote)

	_, err := io.WriteString(w, b.String())
	return err
}

// jsonBodyReport は、取り出しの要らない入力の機械可読出力。ファイルの監査（jsonReport）とは別の形に
// してある——パスも、検査したファイル数も、この入力には無い。無い欄を null で埋めて同じ形に見せると、
// 読み手はそこに値が来ることを期待する。layers と baseline は、当たらない層を機械にも告げるためのもの
// （→ BodyNote）。
type jsonBodyReport struct {
	Version int `json:"version"`

	// Surface は、この入力が当たった面（where.syntax の予約値）。
	Surface string `json:"surface"`

	Layers   jsonBodyLayers  `json:"layers"`
	Baseline bool            `json:"baseline"`
	Summary  jsonBodySummary `json:"summary"`

	Violations []jsonBodyViolation `json:"violations"`
}

// jsonBodyLayers は、この入力に当たった層と、当たらなかった層。
type jsonBodyLayers struct {
	Applied    []string `json:"applied"`
	NotApplied []string `json:"not_applied"`
}

type jsonBodySummary struct {
	Violations int `json:"violations"`
}

// jsonBodyViolation の line は、入力の中の行（当たった段落の先頭）。
type jsonBodyViolation struct {
	Line          int    `json:"line"`
	ID            string `json:"id"`
	Text          string `json:"text"`
	Message       string `json:"message"`
	SeamDependent bool   `json:"seam_dependent,omitempty"`
}

// BodyJSON は、取り出しの要らない入力の違反を機械可読で書く。violations は、空でも null でなく空配列。
func BodyJSON(w io.Writer, vs []rule.Violation) error {
	out := jsonBodyReport{
		Version: 1,
		Surface: "text",
		Layers: jsonBodyLayers{
			Applied:    []string{"lexicon"},
			NotApplied: []string{"vessel", "form"},
		},
		Baseline:   false,
		Summary:    jsonBodySummary{Violations: len(vs)},
		Violations: make([]jsonBodyViolation, 0, len(vs)),
	}
	for _, v := range vs {
		out.Violations = append(out.Violations, jsonBodyViolation{
			Line:          v.Line,
			ID:            v.ID,
			Text:          v.Text,
			Message:       strings.TrimRight(v.Message, "\n"),
			SeamDependent: v.SeamDependent,
		})
	}
	return encode(w, out)
}
