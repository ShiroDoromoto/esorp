package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runAgent は、esorp をどう使うかの地図を書き出す。層3（review）に答えるのはエージェント自身
// なので、その口の存在と使い方が見つからなければ層3 は開かないままになる。ここが、エージェントに
// とっての唯一の真実。text と JSON は同じ地図から書き出す（二重に持たない）。
func runAgent(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "出力の形式（text | json）")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp agent: 余分な引数 %q\n", fs.Arg(0))
		return exitConfig
	}
	if !knownFormat("agent", *format, stderr) {
		return exitConfig
	}

	m := agentMap()
	if *format == "json" {
		if err := encodeAgent(stdout, m); err != nil {
			fmt.Fprintf(stderr, "esorp: %v\n", err)
			return exitConfig
		}
		return exitOK
	}
	if err := writeAgentText(stdout, m); err != nil {
		fmt.Fprintf(stderr, "esorp: %v\n", err)
		return exitConfig
	}
	return exitOK
}

// agentDoc は、esorp の使い方の地図。version は、この形を変えたときに読み手が気づけるようにある。
type agentDoc struct {
	Version   int            `json:"version"`
	Tool      string         `json:"tool"`
	Summary   string         `json:"summary"`
	Purpose   string         `json:"purpose"`
	Layers    []agentLayer   `json:"layers"`
	Cycle     []string       `json:"cycle"`
	Commands  []agentCommand `json:"commands"`
	Output    agentOutput    `json:"output"`
	ExitCodes []agentExit    `json:"exit_codes"`
	Rules     []string       `json:"rules"`
}

// agentLayer は、何をどこまで機械が決めるかの線。Who が「誰が答えるか」で、層3 だけがエージェント。
type agentLayer struct {
	Layer         int    `json:"layer"`
	Name          string `json:"name"`
	Sees          string `json:"sees"`
	Deterministic bool   `json:"deterministic"`
	Who           string `json:"who"`
}

type agentCommand struct {
	Command string `json:"command"`
	What    string `json:"what"`
	When    string `json:"when"`
}

// agentOutput は check --format json の読み方。violations と review のどちらを見るかで、
// エージェントのすることが変わる。
type agentOutput struct {
	Command    string `json:"command"`
	Violations string `json:"violations"`
	Review     string `json:"review"`
	Closed     string `json:"review_closed"`
}

type agentExit struct {
	Code    int    `json:"code"`
	Meaning string `json:"meaning"`
}

func agentMap() agentDoc {
	return agentDoc{
		Version: 1,
		Tool:    "esorp",
		Summary: "コメントの置き場所と書式を、esorp.yaml の宣言に照らして監査する",
		Purpose: "落とそうとしているのは、目の前のコードの説明ではないコメント——履歴・事情・作業メモ。\n" +
			"書かれた瞬間から陳腐化を始め、コードが変わってもコメントは追随せず、\n" +
			"残ったコメントが将来の読み手（次のエージェントを含む）をミスリードする。",
		Layers: []agentLayer{
			{
				Layer:         1,
				Name:          "器と書式",
				Sees:          "コメントがどこに入っているか、どんな形をしているか",
				Deterministic: true,
				Who:           "esorp。CI と pre-commit がそのまま赤/緑にする",
			},
			{
				Layer:         2,
				Name:          "語彙",
				Sees:          "コメント本文に現れる、そのプロジェクト固有の専用句",
				Deterministic: true,
				Who:           "esorp。ただし語彙を持つのは設定ファイルだけ（init がプリセットを書き込む。消すのも足すのもユーザー）",
			},
			{
				Layer:         3,
				Name:          "意味",
				Sees:          "層1・層2 を通り抜けたコメントが、コードの説明か、事情・履歴・作業メモか",
				Deterministic: false,
				Who:           "あなた（esorp を走らせているエージェント）。esorp は LLM を呼ばず、渡し方だけを持つ",
			},
		},
		Cycle: []string{
			"コードを書き終えたら、コミットする前に `esorp check --diff --format json` を走らせる。\n" +
				"見るのは自分が触った変更分だけで、既存コードの違反は baseline が抑えている。",
			"violations が出たら直す。正解はたいてい削除——履歴はバージョン管理が保持しているし、\n" +
				"違反コメントの大半に移送先など無い。\n" +
				"なぜ違反なのか腑に落ちなければ `esorp explain <path>:<line>` を引く。",
			"review が出ていたら、review.comments の1件ずつに review.question を当てて、あなたが答える。\n" +
				"事情・履歴・作業メモを語っているコメントは、器も形も正しくても消す。\n" +
				"ここは CI が見ない——層3 の網は、あなたしか張れない。",
			"review が出ていなければ、層3 は閉じている（設定に review: が無いか、--diff で絞っていない）。\n" +
				"開くなら esorp.yaml に review.question を書く。",
			"終了コードが 0 になるまで繰り返す。",
		},
		Commands: []agentCommand{
			{
				Command: "esorp check --diff --format json",
				What:    "変更分に現れたコメントだけを監査し、違反と、層3 に渡す材料（review）を機械可読で出す",
				When:    "コミットする前。エージェントとしての主戦場はここ",
			},
			{
				Command: "esorp check --format json",
				What:    "ツリー全体を監査する（review は出ない）",
				When:    "CI と、導入時の現状把握",
			},
			{
				Command: "esorp explain <path>:<line> --format json",
				What:    "その行のコメントが、なぜ違反で、どう始末するのかを、根拠となる設定の該当箇所ごと示す",
				When:    "違反の理由が腑に落ちないとき。check の報告の <path>:<line> をそのまま貼れる",
			},
			{
				Command: "esorp init",
				What:    "設定ファイル（esorp.yaml）を生成する",
				When:    "導入時。生成された設定はその時点でユーザーのもので、esorp は以後それを書き換えない",
			},
			{
				Command: "esorp init --diff --format json",
				What:    "現行テンプレートと手元の設定の差分を出す（書き換えない）",
				When:    "esorp を更新した後。既定ルールの改善を取り込むかどうかは、読んだあなたが決める",
			},
			{
				Command: "esorp baseline update",
				What:    "今ある違反をスナップショットする。載せた違反は報告されないが、一覧として見える状態で残る",
				When:    "導入時に一度。ラチェットは減る方向にしか動かず、CI では使わない",
			},
		},
		Output: agentOutput{
			Command:    "esorp check --diff --format json",
			Violations: "違反。1件ずつに path / line / col / id / place / kind / text / message が付く。message は始末のしかたまで書いてある",
			Review:     "層3 の材料。review.question（あなたに投げる問い）と review.comments（層1・層2 を通り抜けたコメント）。答えはここに無い——出すのはあなた",
			Closed:     "review が無ければ層3 は閉じている。設定に review: が無いか、--diff で絞っていないか、そのどちらか",
		},
		ExitCodes: []agentExit{
			{Code: exitOK, Meaning: "適合"},
			{Code: exitViolated, Meaning: "違反あり（review の中身は終了コードを動かさない）"},
			{Code: exitConfig, Meaning: "設定エラー（設定が読めない・スキーマ違反・使い方の誤り）"},
		},
		Rules: []string{
			"esorp は LLM を呼ばない。層3 の判定はあなたが行う。API キーも課金もネットワークも要らない。",
			"esorp は設定を書き換えない。テンプレートとの差分を見せるところまでがツールの仕事で、\n" +
				"取り込むかどうかは読み手が決める。",
			"インライン抑制コメント（// esorp:ignore）は無い。抑制コメント自体が「許可されていない器の\n" +
				"コメント」になって自己矛盾するし、違反を消す代わりに抑制を足す抜け道になる。\n" +
				"例外は baseline に載せる（＝一覧として見える状態にする）。",
			"esorp はコメントを書き換えない。監査に徹する。直すのはあなた。",
		},
	}
}

// writeAgentText は、同じ地図を散文で書く。JSON を読めない目——人間と、パイプの先を持たない端末——
// にも同じことが伝わるようにする。
func writeAgentText(w io.Writer, m agentDoc) error {
	var b strings.Builder

	fmt.Fprintf(&b, "%s — %s\n\n%s\n", m.Tool, m.Summary, m.Purpose)

	b.WriteString("\n三層のうち、あなたが答えるのは層3 だけです。\n\n")
	for _, l := range m.Layers {
		fmt.Fprintf(&b, "  層%d %s（%s）\n", l.Layer, l.Name, decided(l.Deterministic))
		fmt.Fprintf(&b, "    見るもの: %s\n", l.Sees)
		fmt.Fprintf(&b, "    答える者: %s\n", l.Who)
	}

	b.WriteString("\nあなたのサイクル:\n\n")
	for i, s := range m.Cycle {
		fmt.Fprintf(&b, "  %d. ", i+1)
		continued(&b, s, "     ")
	}

	b.WriteString("\nコマンド:\n\n")
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "  %s\n", c.Command)
		fmt.Fprintf(&b, "    %s\n", c.What)
		fmt.Fprintf(&b, "    いつ: %s\n", c.When)
	}

	fmt.Fprintf(&b, "\n%s の読み方:\n\n", m.Output.Command)
	fmt.Fprintf(&b, "  violations: %s\n", m.Output.Violations)
	fmt.Fprintf(&b, "  review: %s\n", m.Output.Review)
	fmt.Fprintf(&b, "  review が無いとき: %s\n", m.Output.Closed)

	b.WriteString("\n終了コード:\n\n")
	for _, e := range m.ExitCodes {
		fmt.Fprintf(&b, "  %d  %s\n", e.Code, e.Meaning)
	}

	b.WriteString("\n動かない線:\n\n")
	for _, r := range m.Rules {
		b.WriteString("  - ")
		continued(&b, r, "    ")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// continued は、行頭の印（番号・中黒）に続けて複数行の塊を書く。2行目以降は印の幅だけ字下げして、
// どこまでが同じ項目かを見えるようにする。
func continued(b *strings.Builder, s, indent string) {
	for i, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if i > 0 {
			b.WriteString(indent)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func decided(deterministic bool) string {
	if deterministic {
		return "決定論的"
	}
	return "非決定的"
}

func encodeAgent(w io.Writer, m agentDoc) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
