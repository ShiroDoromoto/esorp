// Command esorp は、コメントの置き場所と書式を esorp.yaml の宣言に照らして監査する。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/baseline"
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
  esorp check             設定に従いツリー全体を監査する
  esorp baseline update   既存の違反をスナップショットする（減る方向のみ）
  esorp help              この使い方を表示する

check のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）。この場所がツリーの根になる
  --format <fmt>    出力の形式（text | json、既定: text）

baseline update のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）
  --allow-new       今ある違反を新しく baseline に載せる。CI では使わない

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
	case "baseline":
		return runBaseline(args[1:], stdout, stderr)
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

	a, code := scan(*configPath, stderr)
	if code != exitOK {
		return code
	}
	a.result.Suppress(a.base)

	if err := report.Warnings(stderr, a.result); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	write := report.Text
	if *format == "json" {
		write = report.JSON
	}
	if err := write(stdout, a.result); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	if len(a.result.Findings) > 0 {
		return exitViolated
	}
	return exitOK
}

// runBaseline は baseline のサブコマンドを捌く。今あるのは update だけ。
func runBaseline(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "update" {
		fmt.Fprintf(stderr, "esorp baseline: update を指定してください\n")
		return exitConfig
	}

	fs := flag.NewFlagSet("baseline update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "設定ファイルの場所")
	allowNew := fs.Bool("allow-new", false, "今ある違反を新しく baseline に載せる（CI では使わない）")
	if err := fs.Parse(args[1:]); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp baseline update: 余分な引数 %q\n", fs.Arg(0))
		return exitConfig
	}

	a, code := scan(*configPath, stderr)
	if code != exitOK {
		return code
	}
	if a.baselinePath == "" {
		fmt.Fprintln(stderr, "esorp baseline update: 設定に baseline: がありません")
		return exitConfig
	}

	// ラチェット: 減る方向にしか動かない。もう違反していないキーは落ち、新しい違反は載らない。
	before := a.base.Len()
	entries := a.base.Ratchet(a.result.Entries(), *allowNew)
	if err := baseline.Save(a.baselinePath, entries); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	fmt.Fprintf(stdout, "esorp: baseline を書きました（%d 件 → %d 件 / 今の違反は %d 件）\n",
		before, len(entries), len(a.result.Findings))
	return exitOK
}

// audited は、設定を読んでツリーを走査した結果ひとまとめ。check と baseline update が同じ道を通る。
type audited struct {
	result       *audit.Result
	base         *baseline.Baseline
	baselinePath string
}

// scan は、設定を読み、ツリーを走査し、baseline を読む。baseline はまだ効かせない
// （baseline update は、抑止する前の全違反を要る）。
func scan(configPath string, stderr io.Writer) (*audited, int) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, exitConfig
	}

	// 設定ファイルの置かれた場所が、監査するツリーの根。設定の glob は、ここからの相対パスに当たる。
	root := filepath.Dir(configPath)
	res, err := audit.Run(cfg, root)
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return nil, exitConfig
	}

	path := ""
	if cfg.Baseline != "" {
		path = filepath.Join(root, filepath.FromSlash(cfg.Baseline))
	}
	base, err := baseline.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return nil, exitConfig
	}
	return &audited{result: res, base: base, baselinePath: path}, exitOK
}
