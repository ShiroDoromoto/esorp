// Package report は、検出した違反を人間向け出力と JSON 出力に書き出す。
//
// 人間向けの1件は「どこで・何に反し・何が書かれていて・どう始末するか」で閉じている。
// 設定ファイルを開かなくても、その場で判断できるだけの材料を並べる。
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/audit"
)

// Text は、人間向けの出力を書く。
//
//	internal/store/index.go:8:2  place-not-allowed  place=leading kind=line
//	  // 前方移行はここで行っていた。
//	  この位置のコメントは許可されていません。
func Text(w io.Writer, res *audit.Result) error {
	if len(res.Findings) == 0 {
		_, err := fmt.Fprintf(w, "esorp: no violations (%d files / %d comments%s)\n", res.Files, res.Comments, baselined(res))
		return err
	}

	var b strings.Builder
	for _, f := range res.Findings {
		fmt.Fprintf(&b, "%s:%d:%d  %s  place=%s kind=%s\n", f.Path, f.Line, f.Col, f.ID, f.Place, f.Kind)
		indent(&b, f.Text)
		indent(&b, f.Message)
		if f.SeamDependent {
			indent(&b, SeamNote)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%d violations (%d files / %d comments%s)\n", len(res.Findings), res.Files, res.Comments, baselined(res))

	_, err := io.WriteString(w, b.String())
	return err
}

// SeamNote は、折り返しの継ぎ目に左右される当たりに添える断り。当たった句が、半角と全角の境目で
// 折り返された継ぎ目をまたいでいる。そこに原文の空白が在ったかは復元できないので、この当たりは
// 折り返しが作ったものかもしれない——原文には直す箇所が無いかもしれない。黙って誤爆させないために、
// 検知したうえで、そう告げる。
const SeamNote = `This match depends on a line-wrap seam. The line wrapped at the boundary between half-width and full-width characters,
and whether whitespace stood there in the original cannot be recovered. If there is nothing to fix in the original, put it on the baseline.`

// baselined は、baseline で抑えた件数を添える。抑えているものがあることを、必ず見える所に出す。
func baselined(res *audit.Result) string {
	if res.Baselined == 0 {
		return ""
	}
	return fmt.Sprintf(" / baseline holds down %d", res.Baselined)
}

// indent は、複数行の塊を2つ空けて字下げする。空文字列は何も書かない（disposition は省略できる）。
func indent(b *strings.Builder, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

// jsonReport は機械可読出力の形。version は、この形を変えたときに読み手が気づけるようにある。
type jsonReport struct {
	Version    int             `json:"version"`
	Summary    jsonSummary     `json:"summary"`
	Violations []jsonViolation `json:"violations"`
	Skipped    []string        `json:"skipped"`

	// Review は層3（意味）の材料。設定に review: を書き、変更分に絞った（check --diff）ときだけ
	// 出る。esorp はここに答えを持たない——判定するのは、この出力を読んでいるエージェント自身。
	Review *jsonReview `json:"review,omitempty"`
}

// jsonReview は、層1・層2 を通り抜けたコメントと、それらに投げる問い。
type jsonReview struct {
	Question string        `json:"question"`
	Comments []jsonComment `json:"comments"`
}

type jsonComment struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Place string `json:"place"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

type jsonSummary struct {
	Files      int `json:"files"`
	Comments   int `json:"comments"`
	Violations int `json:"violations"`
	Baselined  int `json:"baselined"`
}

// jsonViolation の seam_dependent は、当たりが折り返しの継ぎ目に左右されること（→ SeamNote）。
// 立たない方が普通なので、立ったときだけ出す。
type jsonViolation struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Col           int    `json:"col"`
	ID            string `json:"id"`
	Place         string `json:"place"`
	Kind          string `json:"kind"`
	Text          string `json:"text"`
	Message       string `json:"message"`
	SeamDependent bool   `json:"seam_dependent,omitempty"`
}

// JSON は、機械可読の出力を書く。violations と skipped は、空でも null でなく空配列にする。
func JSON(w io.Writer, res *audit.Result) error {
	out := jsonReport{
		Version: 2,
		Summary: jsonSummary{
			Files:      res.Files,
			Comments:   res.Comments,
			Violations: len(res.Findings),
			Baselined:  res.Baselined,
		},
		Violations: make([]jsonViolation, 0, len(res.Findings)),
		Skipped:    res.Skipped,
	}
	if out.Skipped == nil {
		out.Skipped = []string{}
	}
	for _, f := range res.Findings {
		out.Violations = append(out.Violations, violation(f))
	}
	if res.Review != nil {
		rv := &jsonReview{
			Question: strings.TrimRight(res.Review.Question, "\n"),
			Comments: make([]jsonComment, 0, len(res.Review.Comments)),
		}
		for _, c := range res.Review.Comments {
			rv.Comments = append(rv.Comments, jsonComment{
				Path:  c.Path,
				Line:  c.Line,
				Col:   c.Col,
				Place: c.Place.String(),
				Kind:  c.Kind.String(),
				Text:  c.Text,
			})
		}
		out.Review = rv
	}

	return encode(w, out)
}

// violation は、違反1件を機械可読の形に直す。check と explain が同じ形で出す（check の JSON で
// 拾った違反を、そのまま explain に渡せる）。
func violation(f audit.Finding) jsonViolation {
	return jsonViolation{
		Path:          f.Path,
		Line:          f.Line,
		Col:           f.Col,
		ID:            f.ID,
		Place:         f.Place.String(),
		Kind:          f.Kind.String(),
		Text:          f.Text,
		Message:       strings.TrimRight(f.Message, "\n"),
		SeamDependent: f.SeamDependent,
	}
}

func encode(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Warnings は、検査できなかったファイルを告げる。設定の files: に当たったのに字句を持っていない
// ファイルは、検査されていない。黙って落とすと「違反はありません」が嘘になるので、必ず見える所に出す。
func Warnings(w io.Writer, skipped []string) error {
	if len(skipped) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "esorp: %d files were not inspected (no scanner for that language yet):\n  %s\n",
		len(skipped), strings.Join(skipped, "\n  "))
	return err
}
