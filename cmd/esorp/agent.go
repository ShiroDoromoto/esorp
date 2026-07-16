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
	format := fs.String("format", "text", "output format (text | json)")
	if err := fs.Parse(args); err != nil {
		return exitConfig
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "esorp agent: unexpected argument %q\n", fs.Arg(0))
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
		Summary: "Audit where comments are placed and how they are written, against the declarations in esorp.yaml",
		Purpose: "What this drops is comments that do not explain the code in front of you — history, circumstances, working notes.\n" +
			"They start going stale the moment they are written; when the code changes the comment does not follow,\n" +
			"and the comment left behind misleads future readers (the next agent included).",
		Layers: []agentLayer{
			{
				Layer:         1,
				Name:          "Vessel and form",
				Sees:          "Where a comment sits, and what shape it has",
				Deterministic: true,
				Who:           "esorp. CI and pre-commit turn it straight into red/green",
			},
			{
				Layer:         2,
				Name:          "Lexicon",
				Sees:          "Project-specific set phrases that appear in a comment's body",
				Deterministic: true,
				Who:           "esorp — but the lexicon lives only in the config file (init writes presets; removing or adding terms is the user's)",
			},
			{
				Layer:         3,
				Name:          "Meaning",
				Sees:          "Whether a comment that passed layers 1 and 2 explains the code, or tells circumstances, history, or working notes",
				Deterministic: false,
				Who:           "You (the agent running esorp). esorp calls no LLM; it only hands the material over",
			},
		},
		Cycle: []string{
			"When you finish writing code, run `esorp check --diff --format json` before you commit.\n" +
				"It looks only at the changes you touched; violations in existing code are held down by the baseline.",
			"If violations appear, fix them. The answer is usually deletion — version control keeps the history,\n" +
				"and most offending comments have nowhere to be moved to.\n" +
				"If you don't see why it is a violation, look it up with `esorp explain <path>:<line>`.",
			"If a review appears, apply review.question to each of review.comments, one by one, and you answer.\n" +
				"A comment that tells circumstances, history, or working notes goes, even when its vessel and form are correct.\n" +
				"CI does not watch here — the layer-3 net is one only you can cast.",
			"If no review appears, layer 3 is closed (the config has no review:, or you did not narrow with --diff).\n" +
				"To open it, write review.question in esorp.yaml.",
			"Repeat until the exit code is 0.",
		},
		Commands: []agentCommand{
			{
				Command: "esorp check --diff --format json",
				What:    "Audit only the comments that appear in the changes, and emit — machine-readable — the violations and the material handed to layer 3 (review)",
				When:    "Before you commit. This is your main battleground as an agent",
			},
			{
				Command: "esorp check --format json",
				What:    "Audit the whole tree (no review is emitted)",
				When:    "CI, and taking stock when you first adopt esorp",
			},
			{
				Command: "esorp explain <path>:<line> --format json",
				What:    "Show why the comment on that line is a violation and how to deal with it, along with the config passage it rests on",
				When:    "When the reason for a violation doesn't land. You can paste the <path>:<line> from check's report as is",
			},
			{
				Command: "esorp init",
				What:    "Generate the config file (esorp.yaml)",
				When:    "At adoption. The generated config is the user's from that moment on, and esorp never rewrites it afterward",
			},
			{
				Command: "esorp init --diff --format json",
				What:    "Emit the diff between the current template and your local config (without rewriting it)",
				When:    "After you update esorp. Whether to take in improvements to the default rules is for you, the reader, to decide",
			},
			{
				Command: "esorp baseline update",
				What:    "Snapshot the violations you have now. A listed violation is not reported, but stays visible as a roster",
				When:    "Once at adoption. The ratchet only ever turns toward fewer, and is not used in CI",
			},
			{
				Command: "esorp review [<path>...]",
				What:    "Gather the comments that passed layers 1 and 2 and emit them together with the config's question. It makes no judgment (you are the one who answers)",
				When:    "Once on your first day. It is the mouth that makes esorp read comments already there; day to day, check --diff's review is enough. If the whole tree is too much, narrow it with <path>",
			},
			{
				Command: "esorp check --text <src> --format json",
				What:    "Read the given string itself as a body and apply only layer 2 (lexicon) (<src> is \"-\" for stdin, otherwise a file path). Layer 1 (vessel and form) does not apply, and there is no baseline (the output's layers / baseline say so)",
				When:    "When you apply the same lexicon to where circumstances driven out of comments take refuge — commit messages, PR bodies, release notes. esorp knows nothing of git, so handing it the body is the caller's job (the commit-msg hook)",
			},
			{
				Command: "esorp lexicon --try <regexp> --format json",
				What:    "Apply a candidate lexicon term to every comment in the tree and emit the count and the bodies it matched. It makes no judgment (reading true positive from false is yours)",
				When:    "Before you add a term to layer 2. A guard that false-positives invites exception markers, and eventually the whole tool gets ignored. Don't add without measuring",
			},
		},
		Output: agentOutput{
			Command:    "esorp check --diff --format json",
			Violations: "Violations. Each carries path / line / col / id / severity / place / kind / text / message. The message even tells you how to deal with it, and severity tells you whether it fails the run (enforce) or is only reported (advisory). summary carries the enforce / advisory breakdown",
			Review:     "The material for layer 3. review.question (the question put to you) and review.comments (the comments that passed layers 1 and 2). The answer is not here — you are the one who produces it",
			Closed:     "If there is no review, layer 3 is closed. Either the config has no review:, or you did not narrow with --diff",
		},
		ExitCodes: []agentExit{
			{Code: exitOK, Meaning: "Conforms — or the only violations left are advisory ones"},
			{Code: exitViolated, Meaning: "An enforced violation is present. Only severity: enforce (the default) moves this code — advisory violations are reported but never fail the run, and neither do the contents of review"},
			{Code: exitConfig, Meaning: "Config error (config unreadable, schema violation, or misuse)"},
		},
		Rules: []string{
			"esorp calls no LLM. The layer-3 judgment is yours to make. No API key, no billing, no network required.",
			"esorp does not rewrite the config. Showing the diff against the template is where the tool's job ends;\n" +
				"whether to take it in is for the reader to decide.",
			"There is no inline suppression comment (// esorp:ignore). A suppression comment would itself become a comment in\n" +
				"an unpermitted vessel — a contradiction — and it would be a loophole for adding a suppression instead of removing the violation.\n" +
				"Put exceptions on the baseline (= keep them visible as a roster).",
			"esorp does not rewrite comments. It stays an audit. Fixing them is yours.",
			"The single source of the forbidden lexicon is esorp.yaml. There is a mouth (check --text) that applies the same lexicon\n" +
				"to commit messages too, so don't write a separate regexp in the hook — a split denylist always drifts.",
		},
	}
}

// writeAgentText は、同じ地図を散文で書く。JSON を読めない目——人間と、パイプの先を持たない端末——
// にも同じことが伝わるようにする。
func writeAgentText(w io.Writer, m agentDoc) error {
	var b strings.Builder

	fmt.Fprintf(&b, "%s — %s\n\n%s\n", m.Tool, m.Summary, m.Purpose)

	b.WriteString("\nOf the three layers, the only one you answer is layer 3.\n\n")
	for _, l := range m.Layers {
		fmt.Fprintf(&b, "  Layer %d %s (%s)\n", l.Layer, l.Name, decided(l.Deterministic))
		fmt.Fprintf(&b, "    Sees: %s\n", l.Sees)
		fmt.Fprintf(&b, "    Answered by: %s\n", l.Who)
	}

	b.WriteString("\nYour cycle:\n\n")
	for i, s := range m.Cycle {
		fmt.Fprintf(&b, "  %d. ", i+1)
		continued(&b, s, "     ")
	}

	b.WriteString("\nCommands:\n\n")
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "  %s\n", c.Command)
		fmt.Fprintf(&b, "    %s\n", c.What)
		fmt.Fprintf(&b, "    When: %s\n", c.When)
	}

	fmt.Fprintf(&b, "\nHow to read %s:\n\n", m.Output.Command)
	fmt.Fprintf(&b, "  violations: %s\n", m.Output.Violations)
	fmt.Fprintf(&b, "  review: %s\n", m.Output.Review)
	fmt.Fprintf(&b, "  when there is no review: %s\n", m.Output.Closed)

	b.WriteString("\nExit codes:\n\n")
	for _, e := range m.ExitCodes {
		fmt.Fprintf(&b, "  %d  %s\n", e.Code, e.Meaning)
	}

	b.WriteString("\nLines that don't move:\n\n")
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
		return "deterministic"
	}
	return "non-deterministic"
}

func encodeAgent(w io.Writer, m agentDoc) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
