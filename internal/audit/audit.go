// Package audit は、設定に従ってファイルを集め、スキャナ → 位置クラス → 照合を回す。
//
// ここが check の骨格であり、CLI はフラグと終了コードだけを持つ。baseline による除外は
// 呼び手が走査の後に挟む（Result.Suppress）。
package audit

import (
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ShiroDoromoto/esorp/internal/baseline"
	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// Finding は違反1件に、それがどのファイルのどの syntax エントリで見つかったかを添えたもの。
// Key は baseline の照合に使う（行番号を含まない → 無関係な編集でずれない）。
type Finding struct {
	// Path はツリーの根からの相対パス。
	Path string

	// Syntax は、当たった syntax エントリの名前。
	Syntax string

	Key string
	rule.Violation
}

// Result は1回の走査の結果。Files と Comments は実際に監査した数（Selection で絞ったなら、
// 絞った後の数）。Skipped は、設定の files: には当たったが、その言語の字句を持っていないので
// 読まなかったファイル（検査されていないことを呼び手が告げるための材料）。
// Baselined は baseline に載っていたので Findings から外した件数。
type Result struct {
	Files     int
	Comments  int
	Findings  []Finding
	Skipped   []string
	Baselined int
}

// Entries は、今の違反を baseline のエントリに写す。baseline update が書き出すもの。
func (r *Result) Entries() []baseline.Entry {
	out := make([]baseline.Entry, 0, len(r.Findings))
	for _, f := range r.Findings {
		out = append(out, baseline.Entry{Key: f.Key, Path: f.Path, ID: f.ID})
	}
	return out
}

// Suppress は、baseline に載っている違反を Findings から外す。
func (r *Result) Suppress(b *baseline.Baseline) {
	kept := r.Findings[:0]
	for _, f := range r.Findings {
		if b.Has(f.Key) {
			r.Baselined++
			continue
		}
		kept = append(kept, f)
	}
	r.Findings = kept
}

// Selection は、監査するコメントを行の範囲で絞る（両端を含む）。
// check --diff が変更行に重なるコメントだけを残すために渡す。nil なら絞らない。
type Selection func(path string, from, to int) bool

// touches は、そのファイルに監査するものが1つでも残るかを見る。
func (s Selection) touches(path string) bool {
	return s == nil || s(path, 1, math.MaxInt)
}

// covers は、from..to 行のコメントを監査するかを見る。
func (s Selection) covers(path string, from, to int) bool {
	return s == nil || s(path, from, to)
}

// Run は、root の下のファイルを設定に照らして監査する。sel が非 nil なら、それに重なる
// コメントだけを監査する（Files / Comments も、絞った後の数を数える）。返すエラーは
// ファイルが読めない類のものだけで、違反は Result に載る（違反はエラーではない）。baseline は
// ここでは効かせない（呼び手が Suppress を呼ぶ）。baseline update は、抑止する前の全違反を要る。
// 違反はパス → 行 → 桁 → id の順に並べる（1つのコメントが複数の書式に反することがあり、id まで
// 見ないと並びが揺れる）。
func Run(cfg *config.Config, root string, sel Selection) (*Result, error) {
	res := &Result{Findings: []Finding{}}

	paths, err := collect(cfg, root, sel)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := auditFile(cfg, root, p, sel, res); err != nil {
			return nil, err
		}
	}

	slices.SortFunc(res.Findings, func(a, b Finding) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := a.Line - b.Line; c != 0 {
			return c
		}
		if c := a.Col - b.Col; c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return res, nil
}

// matched は、1つのファイルと、それを拾った syntax エントリの名前。
type matched struct {
	// path は root からの相対パス。区切りは常に「/」（glob と同じ土俵に乗せる）。
	path string

	syntax string
}

// collect は、root の下を歩き、設定の files: に当たったファイルをパス順に集める。sel が非 nil
// なら、1行も当たらないファイルは読まない。1つのファイルが複数の syntax エントリに当たったときは、
// 名前順で最初のものを採る。設定の書き手が重なりを作らない限り起きないが、起きたときに走査の
// 結果が揺れないようにする。.git の中はソースではないので、設定で除外させるまでもなく落とす。
func collect(cfg *config.Config, root string, sel Selection) ([]matched, error) {
	names := slices.Sorted(maps.Keys(cfg.Syntax))

	var out []matched
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !sel.touches(rel) {
			return nil
		}

		for _, name := range names {
			if matchAny(cfg.Syntax[name].Files, rel) {
				out = append(out, matched{path: rel, syntax: name})
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s を歩けません: %w", root, err)
	}
	return out, nil
}

// matchAny は、glob のいずれかがパスに当たるかを見る。** を含む照合は doublestar に任せる。
func matchAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, path); err == nil && ok {
			return true
		}
	}
	return false
}

// auditFile は、ファイル1つを読み、器 → 書式 の順に検査して違反を Result に足す（mode:
// content-only は器を問わないので、層2 が入るまで見るものが無い）。同じ本文の同じ違反が1つの
// ファイルに何度も現れる（型の全フィールドに付いた同じ行末コメントなど）ため、baseline のキーは
// 出現順で区別する。sel で落ちるコメントも照合までは回し、出現順だけ進めて報告しない。ここを
// 飛ばすと、同じ違反のキーが check と check --diff で食い違い、baseline が効かなくなる。
func auditFile(cfg *config.Config, root string, m matched, sel Selection, res *Result) error {
	spec, ok := scan.SpecFor(m.path)
	if !ok {
		res.Skipped = append(res.Skipped, m.path)
		return nil
	}

	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(m.path)))
	if err != nil {
		return fmt.Errorf("%s を読めません: %w", m.path, err)
	}

	syn := cfg.Syntax[m.syntax]
	comments := place.Classify(scan.CStyle(src, spec), spec)
	res.Files++
	for _, c := range comments {
		if sel.covers(m.path, c.Line, c.EndLine) {
			res.Comments++
		}
	}

	if syn.Mode != "structural" {
		return nil
	}

	occurrence := map[string]int{}
	add := func(c place.Comment, v rule.Violation) {
		body := scan.Body(c.Text, spec)
		seed := v.ID + "\x00" + body
		key := baseline.Key(m.path, v.ID, body, occurrence[seed])
		occurrence[seed]++
		if !sel.covers(m.path, c.Line, c.EndLine) {
			return
		}
		res.Findings = append(res.Findings, Finding{Path: m.path, Syntax: m.syntax, Key: key, Violation: v})
	}

	for _, c := range comments {
		a, v := rule.Vessel(c, syn.Allow, cfg.Disposition, spec)
		if v != nil {
			add(c, *v)
			continue
		}
		for _, fv := range rule.Form(c, a.Form, cfg.Disposition, spec) {
			add(c, fv)
		}
	}
	return nil
}
