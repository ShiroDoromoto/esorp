// Command esorp は、コメントの置き場所と書式を esorp.yaml の宣言に照らして監査する。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/report"
)

// 終了コードは CI と pre-commit hook がそのまま解釈する契約であり、
// どのサブコマンドもこの3値以外を返さない。
const (
	exitOK       = 0 // 適合
	exitViolated = 1 // 違反あり
	exitConfig   = 2 // 設定エラー（設定が読めない・スキーマ違反・使い方の誤り・ファイルが読めない）
)

const usage = `esorp — コメントの置き場所と書式を監査する

使い方:
  esorp check    設定に従いツリー全体を監査する
  esorp help     この使い方を表示する

check のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）。この場所がツリーの根になる
  --format <fmt>    出力の形式（text | json、既定: text）

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
	configPath := fs.String("config", "esorp.yaml", "設定ファイルの場所")
	format := fs.String("format", "text", "出力の形式（text | json）")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp check: 余分な引数 %q（監査するツリーは --config の場所で決まります）\n", fs.Arg(0))
		return exitConfig
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "esorp check: --format %q は不明です（text | json）\n", *format)
		return exitConfig
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitConfig
	}

	// 設定ファイルの置かれた場所が、監査するツリーの根。設定の glob は、ここからの相対パスに当たる。
	res, err := audit.Run(cfg, filepath.Dir(*configPath))
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	if err := report.Warnings(stderr, res); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	write := report.Text
	if *format == "json" {
		write = report.JSON
	}
	if err := write(stdout, res); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	if len(res.Findings) > 0 {
		return exitViolated
	}
	return exitOK
}
