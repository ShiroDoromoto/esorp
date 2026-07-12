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
//	internal/scan/cstyle.go:42:2  place-not-allowed  place=leading kind=line
//	  // 以前はここで前方移行していた
//	  この位置のコメントは許可されていません。
func Text(w io.Writer, res *audit.Result) error {
	if len(res.Findings) == 0 {
		_, err := fmt.Fprintf(w, "esorp: 違反はありません（%d ファイル / %d コメント%s）\n", res.Files, res.Comments, baselined(res))
		return err
	}

	var b strings.Builder
	for _, f := range res.Findings {
		fmt.Fprintf(&b, "%s:%d:%d  %s  place=%s kind=%s\n", f.Path, f.Line, f.Col, f.ID, f.Place, f.Kind)
		indent(&b, f.Text)
		indent(&b, f.Message)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%d 件の違反（%d ファイル / %d コメント%s）\n", len(res.Findings), res.Files, res.Comments, baselined(res))

	_, err := io.WriteString(w, b.String())
	return err
}

// baselined は、baseline で抑えた件数を添える。抑えているものがあることを、必ず見える所に出す。
func baselined(res *audit.Result) string {
	if res.Baselined == 0 {
		return ""
	}
	return fmt.Sprintf(" / baseline が %d 件を抑えています", res.Baselined)
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
}

type jsonSummary struct {
	Files      int `json:"files"`
	Comments   int `json:"comments"`
	Violations int `json:"violations"`
	Baselined  int `json:"baselined"`
}

type jsonViolation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	ID      string `json:"id"`
	Place   string `json:"place"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Message string `json:"message"`
}

// JSON は、機械可読の出力を書く。violations と skipped は、空でも null でなく空配列にする。
func JSON(w io.Writer, res *audit.Result) error {
	out := jsonReport{
		Version: 1,
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
		out.Violations = append(out.Violations, jsonViolation{
			Path:    f.Path,
			Line:    f.Line,
			Col:     f.Col,
			ID:      f.ID,
			Place:   f.Place.String(),
			Kind:    f.Kind.String(),
			Text:    f.Text,
			Message: strings.TrimRight(f.Message, "\n"),
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Warnings は、検査できなかったファイルを告げる。
//
// 設定の files: に当たったのに字句を持っていないファイルは、検査されていない。黙って落とすと
// 「違反はありません」が嘘になるので、必ず見える所に出す。
func Warnings(w io.Writer, res *audit.Result) error {
	if len(res.Skipped) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "esorp: %d ファイルを検査していません（その言語のスキャナがまだありません）:\n  %s\n",
		len(res.Skipped), strings.Join(res.Skipped, "\n  "))
	return err
}
