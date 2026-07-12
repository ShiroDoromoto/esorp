// Command esorp は、コメントの置き場所と書式を esorp.yaml の宣言に照らして監査する。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// 終了コードは CI と pre-commit hook がそのまま解釈する契約であり、
// どのサブコマンドもこの3値以外を返さない。
const (
	exitOK       = 0 // 適合
	exitViolated = 1 // 違反あり
	exitConfig   = 2 // 設定エラー（設定が読めない・スキーマ違反・使い方の誤り）
)

const usage = `esorp — コメントの置き場所と書式を監査する

使い方:
  esorp check    設定に従いツリー全体を監査する
  esorp help     この使い方を表示する

終了コード:
  0  適合
  1  違反あり
  2  設定エラー
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitConfig
	}

	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "esorp: 未知のサブコマンド %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitConfig
	}
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}

	// 走査・照合・出力は後続タスクで入る。今は違反ゼロとして適合を返す。
	fmt.Fprintln(stdout, "esorp: 違反はありません")
	return exitOK
}
