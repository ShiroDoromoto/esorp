package scan

import "strings"

// Body は、コメントの生テキストから記号を剥がして本文だけにする。
//
// 剥がすのは、行コメント・ブロックコメント・doc 記法の記号と、ブロックコメントの継ぎ行に
// 添えられる「*」、そして前後の空白。本文そのものには手を触れない。
// ラベルの判定（rule）と baseline のキー計算（baseline）が、これを共通の入口にする。
func Body(text string, spec LangSpec) string {
	openers := commentOpeners(spec)

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if spec.BlockClose != "" {
			line = strings.TrimSpace(strings.TrimSuffix(line, spec.BlockClose))
		}
		line = trimLongestPrefix(line, openers)
		// ブロックコメントの継ぎ行に添えられる「*」。
		if rest, ok := strings.CutPrefix(line, "*"); ok {
			line = rest
		}
		lines = append(lines, strings.TrimSpace(line))
	}

	// 記号だけの行（/* や */ の行）が前後に残るので落とす。中の空行は段落の区切りなので残す。
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// BodyLines は、コメント記号だけを剥がし、行頭の字下げを残した本文の行を返す。
// 字下げを落とす Body では、doc のコードブロックと散文を見分けられない（→ IsCodeLine）。
func BodyLines(text string, spec LangSpec) []string {
	openers := commentOpeners(spec)

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		s := line
		if spec.BlockClose != "" {
			if t := strings.TrimRight(s, " \t"); strings.HasSuffix(t, spec.BlockClose) {
				s = strings.TrimSuffix(t, spec.BlockClose)
			}
		}
		s = strings.TrimLeft(s, " \t")
		s = trimLongestPrefix(s, openers)
		if rest, ok := strings.CutPrefix(s, "*"); ok {
			s = rest
		}
		s = strings.TrimPrefix(s, " ")
		lines = append(lines, strings.TrimRight(s, " \t"))
	}

	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// IsCodeLine は、doc の中のコードブロックの行であることを表す。見分けるのはタブだけ。
func IsCodeLine(line string) bool {
	return strings.HasPrefix(line, "\t")
}

// commentOpeners は、剥がすべきコメント記号を長い順に返す。/// は // を接頭辞に含む。
func commentOpeners(spec LangSpec) []string {
	openers := make([]string, 0, len(spec.DocLine)+len(spec.DocBlock)+2)
	openers = append(openers, spec.DocLine...)
	openers = append(openers, spec.DocBlock...)
	if spec.LineComment != "" {
		openers = append(openers, spec.LineComment)
	}
	if spec.BlockOpen != "" {
		openers = append(openers, spec.BlockOpen)
	}
	return openers
}

func trimLongestPrefix(s string, prefixes []string) string {
	best := ""
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) && len(p) > len(best) {
			best = p
		}
	}
	return strings.TrimPrefix(s, best)
}
