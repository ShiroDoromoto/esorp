// Command esorp は、コメントの置き場所と書式を esorp.yaml の宣言に照らして監査する。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ShiroDoromoto/esorp/internal/audit"
	"github.com/ShiroDoromoto/esorp/internal/baseline"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/diff"
	"github.com/ShiroDoromoto/esorp/internal/report"
)

// 終了コードは CI と pre-commit hook がそのまま解釈する契約であり、
// どのサブコマンドもこの3値以外を返さない。
const (
	// exitOK は適合。
	exitOK = 0

	// exitViolated は違反あり。
	exitViolated = 1

	// exitConfig は設定エラー（設定が読めない・スキーマ違反・使い方の誤り・ファイルが読めない）。
	exitConfig = 2
)

// defaultRef は --diff の比較先の既定。CI でも pre-commit でも、既定の関心は「main から見た自分の変更」。
const defaultRef = "origin/HEAD"

const usage = `esorp — コメントの置き場所と書式を監査する

使い方:
  esorp init                 設定ファイル（esorp.yaml）を生成する
  esorp init --diff          現行テンプレートと手元の設定の差分を見せる（書き換えない）
  esorp check                設定に従いツリー全体を監査する
  esorp check --diff [<ref>] 変更分のみ監査する（既定の <ref> は origin/HEAD）
  esorp explain <file>:<line>  その行のコメントが、なぜ違反で、どう始末するのかを説明する
  esorp baseline update      既存の違反をスナップショットする（減る方向のみ）
  esorp lexicon --try <re>   層2 に足す前に、候補の語彙を自分のコーパスで測る
  esorp review [<path>...]   層1・層2 を通り抜けたコメントを、問いを添えてまとめて渡す（層3）
  esorp agent                エージェント向けの入口（層3 に答えるのは、あなた）
  esorp help                 この使い方を表示する

init のフラグ:
  --config <path>   生成する場所（既定: esorp.yaml）。--diff では比べる相手
  --force           既にある設定ファイルを上書きする
  --diff            現行テンプレートと手元の設定の差分を見せる。設定は生成された時点で
                    あなたのものなので、ツールを更新しても勝手には変わらない。既定ルールの
                    改善は、この口から見て、取り込むかどうかを自分で決める
  --format <fmt>    --diff の出力の形式（text | json、既定: text）

check のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）。この場所がツリーの根になる
  --format <fmt>    出力の形式（text | json、既定: text）
  --diff            <ref> と HEAD の分岐点から作業ツリーまでに追加された行に重なる
                    コメントだけを監査する（pre-commit / PR 向け）。baseline も併せて効く。
                    <ref> は末尾に置く（他のフラグは <ref> より前に並べる）

  設定に review: を書いてあると、--diff かつ --format json のときだけ、層1・層2 を通り抜けた
  コメントと、それらに投げる問いが review として出る（層3）。esorp は意味を判定せず、LLM も
  呼ばない。答えるのは、この出力を読んでいるエージェント自身。終了コードは変わらない。

explain のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）
  --format <fmt>    出力の形式（text | json、既定: text）

  <file>:<line> は check の報告をそのまま貼れる（桁まで付いていても受ける）。
  違反そのものに加え、それを決めた設定の該当箇所（allow の列挙 / form / rules）を指す。

baseline update のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）
  --allow-new       今ある違反を新しく baseline に載せる。CI では使わない

lexicon のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）。この場所がツリーの根になる
  --try <re>        当てる候補パターン（Go の正規表現。rules: の pattern と同じ書き方）
  --format <fmt>    出力の形式（text | json、既定: text）

  層2 の語彙を足す前に、それが自分のコーパスでどれだけ誤検知するかを測る口。当てる本文は層2 と
  同じ（折り返しを畳んだもの）なので、ここで出た件数が、足したときに当たる件数そのもの。
  真陽性か偽陽性かは判定しない——当たりを読んで決めるのは、あなた。違反ではないので、当たっても
  終了コードは 0。

review のフラグ:
  --config <path>   設定ファイルの場所（既定: esorp.yaml）。この場所がツリーの根になる
  --format <fmt>    出力の形式（text | json、既定: json）

  フラグは <path> より前に置く（後ろのフラグはパスとして読まれるので、エラーにします）。

  層1・層2 のどれにも当たらなかったコメントを、設定の review.question を添えて渡す口。
  check --diff が「今書いたもの」を渡すのに対し、こちらは既にあるツリーを渡す——導入初日に、
  既存のコメントを一度だけエージェントに読ませるためにある。<path> を与えると、そこに入る
  コメントだけを渡す（ツリー全体を無制限に吐くと、読む側が破綻する）。
  esorp は判定しない。だから違反も赤/緑も無く、終了コードは 0 のまま（層3 は CI に関与しない）。

agent のフラグ:
  --format <fmt>    出力の形式（text | json、既定: text）

  esorp を走らせている AI エージェントが読む口。三層のどこを誰が答えるか、どのコマンドを
  いつ使うか、出力のどこを見るかを、一箇所にまとめて出す。

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
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "explain":
		return runExplain(args[1:], stdout, stderr)
	case "baseline":
		return runBaseline(args[1:], stdout, stderr)
	case "lexicon":
		return runLexicon(args[1:], stdout, stderr)
	case "review":
		return runReview(args[1:], stdout, stderr)
	case "agent":
		return runAgent(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "esorp: 未知のサブコマンド %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitConfig
	}
}

// runInit は設定ファイルを生成する。生成された設定はその時点でユーザーのものになり、ツールを
// 更新しても勝手には変わらない。だから既にあるものを黙って上書きしない。
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "生成する場所")
	force := fs.Bool("force", false, "既にある設定ファイルを上書きする")
	diffMode := fs.Bool("diff", false, "現行テンプレートと手元の設定の差分を見せる")
	format := fs.String("format", "text", "差分の出力の形式（text | json）")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp init: 余分な引数 %q（生成する場所は --config で指定します）\n", fs.Arg(0))
		return exitConfig
	}
	if !knownFormat("init", *format, stderr) {
		return exitConfig
	}
	if *diffMode {
		return runInitDiff(*configPath, *format, stdout, stderr)
	}
	if *format != "text" {
		fmt.Fprintf(stderr, "esorp init: --format は --diff の出力の形式です（設定の生成は書くだけで、出力を持ちません）\n")
		return exitConfig
	}

	if _, err := os.Stat(*configPath); err == nil && !*force {
		fmt.Fprintf(stderr, "esorp init: %s は既にあります（上書きするなら --force）\n", *configPath)
		return exitConfig
	}
	if err := os.WriteFile(*configPath, []byte(config.Template), 0o644); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	fmt.Fprintf(stdout, "esorp: %s を書きました。使わない言語のエントリは削ってください\n", *configPath)
	fmt.Fprint(stdout, initNextSteps)
	return exitOK
}

// initNextSteps は、生成した設定で最初の check を赤で殴らないための導線。層2 のプリセットは
// 過去に書かれたコメントにも当たるので、既存のツリーでは初回が赤くなる。赤で殴られたユーザーは
// ガードごと無視するようになるため、まず今ある違反を baseline に載せて、そこから増やさない。
// review の案内が条件付きなのは、テンプレートが review: をコメントアウトして吐くからで（層3 は
// 既定を持たない）、書いていない人に、開かない口を勧めない。
const initNextSteps = `
既にコメントのあるツリーなら、初回の check は過去のコメントに当たって赤くなります。
今ある違反をスナップショットしてから始めてください（減る方向にしか動きません）:

    esorp baseline update --allow-new    今ある違反を .esorp-baseline.json に載せる
    esorp check                          ここから増やさない

設定の review: を有効にしたなら、層1・層2 を通り抜けた既存のコメントを、導入初日に一度だけ
エージェントに読ませられます（判定はしません。常に 0 で終わります）:

    esorp review <path>    そこに入るコメントを、問いを添えてまとめて渡す
`

// runInitDiff は、現行テンプレートと手元の設定の差分を見せる。設定は生成された時点でユーザーのもの
// なので、ツールを更新しても勝手には変わらない。既定ルールの改善を届ける口はここだけで、取り込むか
// どうかはユーザーが決める。差分があっても、それは違反ではないので 0 で終わる。
func runInitDiff(configPath, format string, stdout, stderr io.Writer) int {
	local, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	tmpl, err := config.TemplateConfig()
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	sections := config.Diff(local, tmpl)
	if format == "json" {
		if err := report.DiffJSON(stdout, configPath, sections); err != nil {
			fmt.Fprintf(stderr, "esorp: %v\n", err)
			return exitConfig
		}
		return exitOK
	}

	if len(sections) == 0 {
		fmt.Fprintf(stdout, "esorp: %s は現行テンプレートと同じです\n", configPath)
		return exitOK
	}

	fmt.Fprintf(stdout, "%s と現行テンプレートの差分です。\n", configPath)
	for _, s := range sections {
		fmt.Fprintf(stdout, "\n%s\n", s.Title)
		for _, line := range s.Lines() {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
	}
	fmt.Fprint(stdout, "\n取り込むかどうかはあなたが決めます。esorp は設定を書き換えません。\n")
	return exitOK
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "設定ファイルの場所")
	format := fs.String("format", "text", "出力の形式（text | json）")
	diffMode := fs.Bool("diff", false, "変更行に重なるコメントだけを監査する")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if !knownFormat("check", *format, stderr) {
		return exitConfig
	}

	ref := defaultRef
	switch {
	case !*diffMode && fs.NArg() > 0:
		fmt.Fprintf(stderr, "esorp check: 余分な引数 %q（監査するツリーは --config の場所で決まります）\n", fs.Arg(0))
		return exitConfig
	case fs.NArg() > 1:
		fmt.Fprintf(stderr, "esorp check --diff: 余分な引数 %q（取るのは比較先の <ref> 1つだけです）\n", fs.Arg(1))
		return exitConfig
	case fs.NArg() == 1:
		ref = fs.Arg(0)
	}

	var sel audit.Selection
	if *diffMode {
		ranges, err := diff.Changed(filepath.Dir(*configPath), ref)
		if err != nil {
			fmt.Fprintf(stderr, "esorp: %v\n", err)
			return exitConfig
		}
		sel = ranges.Overlaps
	}

	a, code := scan(*configPath, sel, *diffMode, stderr)
	if code != exitOK {
		return code
	}
	a.result.Suppress(a.base)

	if err := report.Warnings(stderr, a.result.Skipped); err != nil {
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

// knownFormat は --format を検める。text と json のどちらでもない指定は、黙って text に落とさない。
func knownFormat(cmd, format string, stderr io.Writer) bool {
	if format != "text" && format != "json" {
		fmt.Fprintf(stderr, "esorp %s: --format %q は不明です（text | json）\n", cmd, format)
		return false
	}
	return true
}

// runExplain は、指し示された行のコメントについて、違反とその根拠を書く。違反を「禁止」とだけ
// 伝えると、書き手は言い換えて再投稿する。何がその器を許していないのかまで見せて、はじめて直せる。
// 監査そのものは check と同じ道を通り、絞り込みだけを「その行に重なるコメント」にする（--diff が
// 変更行で絞るのと同じ仕組み）。baseline は効かせない（抑えている違反も、問われたなら説明する）。
func runExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "設定ファイルの場所")
	format := fs.String("format", "text", "出力の形式（text | json）")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if !knownFormat("explain", *format, stderr) {
		return exitConfig
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "esorp explain: 説明するコメントを <file>:<line> で1つ指定してください\n")
		return exitConfig
	}

	file, line, ok := parseTarget(fs.Arg(0))
	if !ok {
		fmt.Fprintf(stderr, "esorp explain: %q は <file>:<line> の形ではありません\n", fs.Arg(0))
		return exitConfig
	}

	root := filepath.Dir(*configPath)
	rel, err := locate(root, file)
	if err != nil {
		fmt.Fprintf(stderr, "esorp explain: %v\n", err)
		return exitConfig
	}

	sel := audit.Selection(func(p string, from, to int) bool {
		return p == rel && from <= line && line <= to
	})
	a, code := scan(*configPath, sel, false, stderr)
	if code != exitOK {
		return code
	}

	switch {
	case len(a.result.Skipped) > 0:
		fmt.Fprintf(stderr, "esorp explain: %s はまだ検査できません（その言語のスキャナがありません）\n", rel)
		return exitConfig
	case a.result.Files == 0:
		fmt.Fprintf(stderr, "esorp explain: %s は監査の対象ではありません（syntax.files: に当たらないか、gitignore されています）\n", rel)
		return exitConfig
	}

	if *format == "json" {
		if err := report.ExplainJSON(stdout, a.cfg, *configPath, rel, line, a.result, a.base); err != nil {
			fmt.Fprintf(stderr, "esorp: %v\n", err)
			return exitConfig
		}
		return explainCode(a.result)
	}

	switch {
	case a.result.Comments == 0:
		fmt.Fprintf(stdout, "esorp: %s:%d にコメントはありません\n", rel, line)
		return exitOK
	case len(a.result.Findings) == 0:
		fmt.Fprintf(stdout, "esorp: %s:%d のコメントは設定に適合しています\n", rel, line)
		return exitOK
	}

	if err := report.Explain(stdout, a.cfg, *configPath, a.result, a.base); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	return exitViolated
}

// explainCode は、説明した違反の有無を終了コードにする。形式で終了コードは変わらない。
func explainCode(res *audit.Result) int {
	if len(res.Findings) > 0 {
		return exitViolated
	}
	return exitOK
}

// parseTarget は「<file>:<line>」を割る。check の報告（<file>:<line>:<col>）をそのまま貼れるよう、
// 桁まで付いた形も受ける。桁は使わない（説明するのはコメント1つであって、その中の1文字ではない）。
func parseTarget(arg string) (string, int, bool) {
	parts := strings.Split(arg, ":")
	if len(parts) >= 3 && isNumber(parts[len(parts)-1]) && isNumber(parts[len(parts)-2]) {
		parts = parts[:len(parts)-1]
	}
	if len(parts) < 2 {
		return "", 0, false
	}

	line, err := strconv.Atoi(parts[len(parts)-1])
	file := strings.Join(parts[:len(parts)-1], ":")
	if err != nil || line < 1 || file == "" {
		return "", 0, false
	}
	return file, line, true
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// locate は、与えられたパスを監査するツリーの根からの相対パスに直す。check の報告に出るパス（根
// からの相対）でも、手元の相対パス・絶対パスでも、同じコメントを指せるようにする。
func locate(root, file string) (string, error) {
	if rel := filepath.ToSlash(file); readable(filepath.Join(root, filepath.FromSlash(rel))) {
		return rel, nil
	}

	abs, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)

	switch {
	case strings.HasPrefix(rel, "../"):
		return "", fmt.Errorf("%s は監査するツリー（%s）の外です", file, root)
	case !readable(abs):
		return "", fmt.Errorf("%s がありません", file)
	}
	return rel, nil
}

func readable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// runBaseline は baseline のサブコマンドを捌く。今あるのは update だけで、書き出しはラチェットを
// 通す（減る方向にしか動かない。もう違反していないキーは落ち、新しい違反は載らない）。
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

	a, code := scan(*configPath, nil, false, stderr)
	if code != exitOK {
		return code
	}
	if a.baselinePath == "" {
		fmt.Fprintln(stderr, "esorp baseline update: 設定に baseline: がありません")
		return exitConfig
	}

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

// runLexicon は、候補の語彙を自分のコーパスに当てて見せる。設定も README も「足す前に測れ」と言う
// が、測る手段が無ければ「稀なら足さない方がまし」は守りようがない。当たりを見せるだけで、真陽性か
// 偽陽性かは判定しない（読むのは人間、あるいは層3 のエージェント）。当たっても違反ではないので、
// 終了コードは 0 のまま——CI を赤くする口ではない。
func runLexicon(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lexicon", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "設定ファイルの場所")
	format := fs.String("format", "text", "出力の形式（text | json）")
	try := fs.String("try", "", "当てる候補パターン（Go の正規表現）")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp lexicon: 余分な引数 %q（測るツリーは --config の場所で決まります）\n", fs.Arg(0))
		return exitConfig
	}
	if !knownFormat("lexicon", *format, stderr) {
		return exitConfig
	}
	if *try == "" {
		fmt.Fprintln(stderr, "esorp lexicon: --try <パターン> を指定してください")
		return exitConfig
	}

	re, err := regexp.Compile(*try)
	if err != nil {
		fmt.Fprintf(stderr, "esorp lexicon: --try のパターンが正規表現として読めません: %v\n", err)
		return exitConfig
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitConfig
	}

	trial, err := audit.Try(cfg, filepath.Dir(*configPath), re)
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	if err := report.Warnings(stderr, trial.Skipped); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	write := report.TryText
	if *format == "json" {
		write = report.TryJSON
	}
	if err := write(stdout, trial); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	return exitOK
}

// runReview は、層1・層2 を通り抜けたコメントを、問いを添えてまとめて渡す。check --diff が「今
// 書いたもの」をエージェントに渡すのに対し、こちらは既にあるツリーを渡す口——導入初日に、既存の
// コメントを一度だけエージェントに読ませるためにある。判定しないので、当たり外れも赤/緑も無く、
// 終了コードは 0 のまま（層3 は CI に関与しない。だから check とコマンドを分けてある）。
// 引数にパスを与えると、そこに入るコメントだけを渡す。ツリー全体を無制限に吐くと、読む側が破綻する。
func runReview(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "設定ファイルの場所")
	format := fs.String("format", "json", "出力の形式（text | json）")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if !knownFormat("review", *format, stderr) {
		return exitConfig
	}
	for _, a := range fs.Args() {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "esorp review: %q はパスとして読まれます（フラグは <path> より前に置いてください）\n", a)
			return exitConfig
		}
	}

	sel := pathSelection(fs.Args())
	a, code := scan(*configPath, sel, true, stderr)
	if code != exitOK {
		return code
	}
	if a.result.Review == nil {
		fmt.Fprintln(stderr, "esorp review: 設定に review: がありません（層3 の口は開いていません）")
		return exitConfig
	}

	if err := report.Warnings(stderr, a.result.Skipped); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	write := report.ReviewText
	if *format == "json" {
		write = report.ReviewJSON
	}
	if err := write(stdout, a.result.Review); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	return exitOK
}

// pathSelection は、引数に与えられたパス（ファイルまたはディレクトリ）に入るコメントだけを選ぶ。
// 引数が無ければ nil を返す（ツリー全体）。
func pathSelection(args []string) audit.Selection {
	if len(args) == 0 {
		return nil
	}
	prefixes := make([]string, 0, len(args))
	for _, a := range args {
		prefixes = append(prefixes, filepath.ToSlash(filepath.Clean(a)))
	}
	return func(path string, _, _ int) bool {
		for _, p := range prefixes {
			if path == p || strings.HasPrefix(path, p+"/") {
				return true
			}
		}
		return false
	}
}

// audited は、設定を読んでツリーを走査した結果ひとまとめ。check / explain / baseline update /
// review が同じ道を通る。
type audited struct {
	cfg          *config.Config
	result       *audit.Result
	base         *baseline.Baseline
	baselinePath string
}

// scan は、設定を読み、ツリーを走査し、baseline を読む。設定ファイルの置かれた場所が、監査する
// ツリーの根（設定の glob は、ここからの相対パスに当たる）。sel は監査するコメントの絞り込み
// （--diff / review のパス指定）で、nil なら絞らない。review は層3 の口を開くかどうか。baseline は
// まだ効かせない（baseline update は、抑止する前の全違反を要る）。
func scan(configPath string, sel audit.Selection, review bool, stderr io.Writer) (*audited, int) {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, exitConfig
	}

	root := filepath.Dir(configPath)
	res, err := audit.Run(cfg, root, sel, review)
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
	return &audited{cfg: cfg, result: res, base: base, baselinePath: path}, exitOK
}
