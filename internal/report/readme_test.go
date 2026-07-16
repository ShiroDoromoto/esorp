package report

import (
	"os"
	"strings"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// readmePath は、引用されたツール出力を照らしにいく先。
const readmePath = "../../README.md"

// readmeBlock は、その sh コマンドを載せた塊の、次のフェンス塊（＝その出力）を返す。
func readmeBlock(t *testing.T, command string) string {
	t.Helper()

	src, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}

	call := "```sh\n" + command + "\n```\n"
	_, after, found := strings.Cut(string(src), call)
	if !found {
		t.Fatalf("README に %q を載せた塊がありません", command)
	}

	open := "\n```\n"
	_, out, found := strings.Cut(after, open)
	if !found {
		t.Fatalf("%q の次にフェンス塊がありません", command)
	}
	body, _, found := strings.Cut(out, "```\n")
	if !found {
		t.Fatalf("%q の次のフェンス塊が閉じていません", command)
	}
	return body
}

// TestReadmeBodyText は、README の check --text のサンプルを BodyText に照らす。
func TestReadmeBodyText(t *testing.T) {
	var b strings.Builder
	err := BodyText(&b, []rule.Violation{{
		ID:       "no-history",
		Line:     1,
		Severity: config.SeverityEnforce,
		Text:     "この関数はかつて同期だった。",
		Message: "変化を語っています。今のコードが何であるかだけを書いてください。\n" +
			"「以前はこうだった」はバージョン管理が保持しています。\n",
	}})
	if err != nil {
		t.Fatal(err)
	}

	wants(t, readmeBlock(t, `printf 'この関数はかつて同期だった。\n' | esorp check --text -`), b.String())
}

// TestReadmeTryText は、README の lexicon --try のサンプルを TryText に照らす。件数（71 ファイル /
// 605 コメント）は例示で、どの実行にも対応しない——面ごとの内訳の合計が全体と合うことだけを保つ。
func TestReadmeTryText(t *testing.T) {
	var b strings.Builder
	err := TryText(&b, &audit.Trial{
		Pattern:  "(?i)previously",
		Files:    71,
		Comments: 605,
		Surfaces: []audit.Surface{
			{Syntax: "cstyle", Files: 60, Comments: 553, Hits: 1},
			{Syntax: "hash", Files: 11, Comments: 52, Hits: 0},
		},
		Hits: []audit.Hit{{
			Path:   "internal/store/index.go",
			Syntax: "cstyle",
			Body:   "Append は、索引の末尾に足す。previously 書き出した領域は読み直さない。",
			Comment: place.Comment{
				Kind: scan.KindLine, Place: place.Doc, Line: 3, Col: 1,
				Text: "// Append は、索引の末尾に足す。previously 書き出した領域は読み直さない。",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	wants(t, readmeBlock(t, `esorp lexicon --try '(?i)previously'`), b.String())
}
