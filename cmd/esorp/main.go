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

const usage = `esorp — audit where comments live and how they read

Usage:
  esorp init                 generate a config file (esorp.yaml)
  esorp init --diff          show the diff between the current template and your config (no rewrite)
  esorp check                audit the whole tree per the config
  esorp check --diff [<ref>] audit only the changed part (default <ref> is origin/HEAD)
  esorp check --text <src>   apply only layer 2 (lexicon) to the given body (for a commit-msg hook;
                             <src> is "-" for stdin, otherwise a file path)
  esorp explain <file>:<line>  explain why the comment on that line violates, and how to settle it
  esorp baseline update      snapshot the existing violations (ratchets down only)
  esorp lexicon --try <re>   measure a candidate term against your own corpus before adding it to layer 2
  esorp review [<path>...]   hand off comments that passed layers 1 and 2, each with a question (layer 3)
  esorp agent                the entry point for agents (you are the one who answers layer 3)
  esorp help                 show this usage

init flags:
  --config <path>   where to generate it (default: esorp.yaml). With --diff, the file to compare against
  --force           overwrite an existing config file
  --diff            show the diff between the current template and your config. The config is yours
                    the moment it is generated, so updating the tool never changes it on its own.
                    This is the one mouth through which improvements to the default rules reach you;
                    you decide whether to take them in
  --format <fmt>    output format for --diff (text | json, default: text)

check flags:
  --config <path>   where the config file is (default: esorp.yaml). This location becomes the tree root
  --format <fmt>    output format (text | json, default: text)
  --diff            audit only the comments overlapping lines added between the merge base of <ref>
                    and HEAD and the working tree (for pre-commit / PR). baseline applies too.
                    Put <ref> last (the other flags come before <ref>)
  --text <src>      read the given string itself as the body, rather than extracting comments from a
                    file. <src> is "-" for stdin, otherwise a file path (its whole content is read as
                    the body; the comment-extraction path is not taken). Only layer 2 (lexicon) applies;
                    layer 1 (container / form) does not — raw text has no container. To scope a rule to
                    this face, use where.syntax: [text] (a rule that omits where.syntax applies here too).
                    baseline does not apply. Cannot be combined with --diff. esorp does not know git, so
                    passing a commit message is the hook's job:

                        esorp check --text - < "$1"    # .git/hooks/commit-msg
                        esorp check --text "$1"        # the same thing, passed by path

  With review: set in the config, and only when both --diff and --format json are given, the comments
  that passed layers 1 and 2 — and the questions to put to them — are emitted as review (layer 3).
  esorp does not judge meaning and does not call an LLM. The one who answers is the agent reading this
  output. The exit code does not change.

explain flags:
  --config <path>   where the config file is (default: esorp.yaml)
  --format <fmt>    output format (text | json, default: text)

  <file>:<line> can be pasted straight from a check report (a trailing column is accepted too).
  Besides the violation itself, it points to the config that decided it (the allow list / form / rules).

baseline update flags:
  --config <path>   where the config file is (default: esorp.yaml)
  --allow-new       add the current violations to the baseline anew. Not for CI

lexicon flags:
  --config <path>   where the config file is (default: esorp.yaml). This location becomes the tree root
  --try <re>        the candidate pattern to apply (a Go regular expression; same syntax as rules: pattern)
  --format <fmt>    output format (text | json, default: text)

  A mouth for measuring, before you add a term to layer 2, how much it misfires on your own corpus. The
  body it applies to is the same as layer 2's (with wraps folded), so the count here is exactly what will
  match once you add it. It does not judge true positive from false — you read the matches and decide.
  A match is not a violation, so the exit code stays 0.

review flags:
  --config <path>   where the config file is (default: esorp.yaml). This location becomes the tree root
  --format <fmt>    output format (text | json, default: json)

  Put the flags before <path> (a flag after it is read as a path, so it errors).

  A mouth for handing off comments that matched none of layers 1 and 2, each with the config's
  review.question. Where check --diff hands off "what you just wrote", this hands off an existing tree —
  it is there to let an agent read the existing comments once, on day one. Given <path>, only the comments
  under it are handed off (dumping the whole tree without bound overwhelms the reader).
  esorp does not judge. So there is no violation, no red/green, and the exit code stays 0 (layer 3 does not touch CI).

agent flags:
  --format <fmt>    output format (text | json, default: text)

  The mouth the AI agent running esorp reads. It lays out, in one place, which of the three layers is
  answered by whom, which command to use when, and where in the output to look.

Exit codes:
  0  conforms
  1  violations found
  2  config error
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run は、標準入力を os.Stdin として捌く（check --text - が読む先）。
func run(args []string, stdout, stderr io.Writer) int {
	return runInput(args, os.Stdin, stdout, stderr)
}

func runInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitConfig
	}

	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdin, stdout, stderr)
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
		fmt.Fprintf(stderr, "esorp: unknown subcommand %q\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return exitConfig
	}
}

// runInit は設定ファイルを生成する。生成された設定はその時点でユーザーのものになり、ツールを
// 更新しても勝手には変わらない。だから既にあるものを黙って上書きしない。
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "where to generate it")
	force := fs.Bool("force", false, "overwrite an existing config file")
	diffMode := fs.Bool("diff", false, "show the diff between the current template and your config")
	format := fs.String("format", "text", "output format for the diff (text | json)")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp init: extra argument %q (specify where to generate it with --config)\n", fs.Arg(0))
		return exitConfig
	}
	if !knownFormat("init", *format, stderr) {
		return exitConfig
	}
	if *diffMode {
		return runInitDiff(*configPath, *format, stdout, stderr)
	}
	if *format != "text" {
		fmt.Fprintf(stderr, "esorp init: --format is the output format for --diff (generating the config only writes; it has no output)\n")
		return exitConfig
	}

	if _, err := os.Stat(*configPath); err == nil && !*force {
		fmt.Fprintf(stderr, "esorp init: %s already exists (pass --force to overwrite)\n", *configPath)
		return exitConfig
	}
	if err := os.WriteFile(*configPath, []byte(config.Template), 0o644); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	fmt.Fprintf(stdout, "esorp: wrote %s. Delete the entries for languages you do not use\n", *configPath)
	fmt.Fprint(stdout, initNextSteps)
	return exitOK
}

// initNextSteps は、生成した設定で最初の check を赤で殴らないための導線。層2 のプリセットは
// 過去に書かれたコメントにも当たるので、既存のツリーでは初回が赤くなる。赤で殴られたユーザーは
// ガードごと無視するようになるため、まず今ある違反を baseline に載せて、そこから増やさない。
// review の案内が条件付きなのは、テンプレートが review: をコメントアウトして吐くからで（層3 は
// 既定を持たない）、書いていない人に、開かない口を勧めない。
const initNextSteps = `
On a tree that already has comments, the first check will hit the past comments and turn red.
Snapshot the current violations before you begin (it only ever ratchets down):

    esorp baseline update --allow-new    put the current violations into .esorp-baseline.json
    esorp check                          go no higher from here

If you enable review: in the config, you can have an agent read the existing comments that pass
layers 1 and 2 once, on day one (it does not judge; it always exits 0):

    esorp review <path>    hand off the comments under it, each with a question
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
		fmt.Fprintf(stdout, "esorp: %s is the same as the current template\n", configPath)
		return exitOK
	}

	fmt.Fprintf(stdout, "The diff between %s and the current template.\n", configPath)
	for _, s := range sections {
		fmt.Fprintf(stdout, "\n%s\n", s.Title)
		for _, line := range s.Lines() {
			fmt.Fprintf(stdout, "  %s\n", line)
		}
	}
	fmt.Fprint(stdout, "\nYou decide whether to take it in. esorp does not rewrite your config.\n")
	return exitOK
}

func runCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "where the config file is")
	format := fs.String("format", "text", "output format (text | json)")
	diffMode := fs.Bool("diff", false, "audit only the comments overlapping changed lines")
	text := fs.String("text", "", "read input that needs no extraction (- is stdin, otherwise a file path)")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if !knownFormat("check", *format, stderr) {
		return exitConfig
	}

	if *text != "" {
		return runCheckBody(*text, *configPath, *format, *diffMode, fs.Args(), stdin, stdout, stderr)
	}

	ref := defaultRef
	switch {
	case !*diffMode && fs.NArg() > 0:
		fmt.Fprintf(stderr, "esorp check: extra argument %q (the tree to audit is set by the --config location)\n", fs.Arg(0))
		return exitConfig
	case fs.NArg() > 1:
		fmt.Fprintf(stderr, "esorp check --diff: extra argument %q (it takes only one <ref> to compare against)\n", fs.Arg(1))
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

	if a.result.Enforced() > 0 {
		return exitViolated
	}
	return exitOK
}

// runCheckBody は、渡された本文に層2（語彙）だけを当てる。コメントから追い出した事情が
// コミットメッセージへ移るのを、同じ esorp.yaml の語彙で止めるための口——禁止語彙をフックにも書けば、
// 源泉が2つに割れてドリフトする。本文は「-」なら標準入力、それ以外はパスとして読む（pre-commit は
// メッセージファイルのパスを引数で渡し、シェルを介さないのでリダイレクトが書けない）。どちらの形でも
// esorp は git を知らず、何を流し込むかは呼び手の裁量にある。ファイルは中身をまるごと本文として読む
// ——コメントを取り出す道は通らないので、層1（器・書式）は当たらない。--diff は変更行との突き合わせで
// あり、渡された本文には比べる相手が無いので弾く。baseline も効かない——この面にパスも行も無く、
// 抑制のキーが立たない。終了コードと --format はツリーの監査と同じで、フックにも CI にも同じ形で挿さる。
func runCheckBody(text, configPath, format string, diffMode bool, rest []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case diffMode:
		fmt.Fprintln(stderr, "esorp check: --text cannot be combined with --diff (the given body has nothing to compare against)")
		return exitConfig
	case len(rest) > 0:
		fmt.Fprintf(stderr, "esorp check --text: extra argument %q (it reads only one body)\n", rest[0])
		return exitConfig
	}

	body, err := readBody(text, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitConfig
	}

	vs := audit.Text(cfg, body)
	write := report.BodyText
	if format == "json" {
		write = report.BodyJSON
	}
	if err := write(stdout, vs); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	if audit.Enforced(vs) > 0 {
		return exitViolated
	}
	return exitOK
}

// readBody は --text の値を本文にする。「-」は標準入力、それ以外はパス。パスを受けるのは、
// pre-commit の commit-msg フックがメッセージファイルを引数で渡すため。どのファイルを渡すかは
// 呼び手が決め、esorp は .git の場所を探しには行かない。
func readBody(text string, stdin io.Reader) (string, error) {
	if text == "-" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("cannot read stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(text)
	if err != nil {
		return "", fmt.Errorf("cannot read the body: %w", err)
	}
	return string(b), nil
}

// knownFormat は --format を検める。text と json のどちらでもない指定は、黙って text に落とさない。
func knownFormat(cmd, format string, stderr io.Writer) bool {
	if format != "text" && format != "json" {
		fmt.Fprintf(stderr, "esorp %s: --format %q is unknown (text | json)\n", cmd, format)
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
	configPath := fs.String("config", "esorp.yaml", "where the config file is")
	format := fs.String("format", "text", "output format (text | json)")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if !knownFormat("explain", *format, stderr) {
		return exitConfig
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "esorp explain: specify one comment to explain as <file>:<line>\n")
		return exitConfig
	}

	file, line, ok := parseTarget(fs.Arg(0))
	if !ok {
		fmt.Fprintf(stderr, "esorp explain: %q is not in the form <file>:<line>\n", fs.Arg(0))
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
		fmt.Fprintf(stderr, "esorp explain: %s cannot be inspected yet (there is no scanner for that language)\n", rel)
		return exitConfig
	case a.result.Files == 0:
		fmt.Fprintf(stderr, "esorp explain: %s is not in scope for the audit (it does not match syntax.files:, or it is gitignored)\n", rel)
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
		fmt.Fprintf(stdout, "esorp: no comment at %s:%d\n", rel, line)
		return exitOK
	case len(a.result.Findings) == 0:
		fmt.Fprintf(stdout, "esorp: the comment at %s:%d conforms to the config\n", rel, line)
		return exitOK
	}

	if err := report.Explain(stdout, a.cfg, *configPath, a.result, a.base); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	return exitViolated
}

// explainCode は、説明した違反の有無を終了コードにする。形式で終了コードは変わらない。
// check とは違い、強度を見ない——advisory の違反にも exitViolated を返す。explain は門ではなく、
// 「このコメントは設定に反しているか」への答えであり、その答えは強度で変わらない（advisory は
// 「反しているが CI は落とさない」であって、「反していない」ではない）。門は check が持つ。
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
		return "", fmt.Errorf("%s is outside the tree being audited (%s)", file, root)
	case !readable(abs):
		return "", fmt.Errorf("%s does not exist", file)
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
		fmt.Fprintf(stderr, "esorp baseline: specify update\n")
		return exitConfig
	}

	fs := flag.NewFlagSet("baseline update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "esorp.yaml", "where the config file is")
	allowNew := fs.Bool("allow-new", false, "add the current violations to the baseline anew (not for CI)")
	if err := fs.Parse(args[1:]); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp baseline update: extra argument %q\n", fs.Arg(0))
		return exitConfig
	}

	a, code := scan(*configPath, nil, false, stderr)
	if code != exitOK {
		return code
	}
	if a.baselinePath == "" {
		fmt.Fprintln(stderr, "esorp baseline update: the config has no baseline:")
		return exitConfig
	}

	before := a.base.Len()
	entries := a.base.Ratchet(a.result.Entries(), *allowNew)
	if err := baseline.Save(a.baselinePath, entries); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}

	fmt.Fprintf(stdout, "esorp: wrote the baseline (%d → %d entries / %d violations now)\n",
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
	configPath := fs.String("config", "esorp.yaml", "where the config file is")
	format := fs.String("format", "text", "output format (text | json)")
	try := fs.String("try", "", "the candidate pattern to apply (a Go regular expression)")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp lexicon: extra argument %q (the tree to measure is set by the --config location)\n", fs.Arg(0))
		return exitConfig
	}
	if !knownFormat("lexicon", *format, stderr) {
		return exitConfig
	}
	if *try == "" {
		fmt.Fprintln(stderr, "esorp lexicon: specify --try <pattern>")
		return exitConfig
	}

	re, err := regexp.Compile(*try)
	if err != nil {
		fmt.Fprintf(stderr, "esorp lexicon: --try's pattern does not parse as a regular expression: %v\n", err)
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
	configPath := fs.String("config", "esorp.yaml", "where the config file is")
	format := fs.String("format", "json", "output format (text | json)")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if !knownFormat("review", *format, stderr) {
		return exitConfig
	}
	for _, a := range fs.Args() {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(stderr, "esorp review: %q is read as a path (put the flags before <path>)\n", a)
			return exitConfig
		}
	}

	sel := pathSelection(fs.Args())
	a, code := scan(*configPath, sel, true, stderr)
	if code != exitOK {
		return code
	}
	if a.result.Review == nil {
		fmt.Fprintln(stderr, "esorp review: the config has no review: (the layer 3 mouth is not open)")
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
