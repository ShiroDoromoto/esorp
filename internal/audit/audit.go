// Package audit は、設定に従ってファイルを集め、スキャナ → 位置クラス → 照合を回す。
//
// ここが check の骨格であり、CLI はフラグと終了コードだけを持つ。baseline による除外と
// 差分モードによる絞り込みは、後続でこの走査の前後に挟まる。
package audit

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/rule"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// Finding は違反1件に、それがどのファイルのどの syntax エントリで見つかったかを添えたもの。
type Finding struct {
	Path   string // ツリーの根からの相対パス
	Syntax string // 当たった syntax エントリの名前
	rule.Violation
}

// Result は1回の走査の結果。Skipped は、設定の files: には当たったが、その言語の字句を
// 持っていないので読まなかったファイル（検査されていないことを呼び手が告げるための材料）。
type Result struct {
	Files    int
	Comments int
	Findings []Finding
	Skipped  []string
}

// Run は、root の下のファイルを設定に照らして監査する。
//
// 返すエラーはファイルが読めない類のものだけで、違反は Result に載る（違反はエラーではない）。
func Run(cfg *config.Config, root string) (*Result, error) {
	res := &Result{Findings: []Finding{}}

	paths, err := collect(cfg, root)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := auditFile(cfg, root, p, res); err != nil {
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
		return a.Col - b.Col
	})
	return res, nil
}

// matched は、1つのファイルと、それを拾った syntax エントリの名前。
type matched struct {
	path   string // root からの相対パス。区切りは常に「/」（glob と同じ土俵に乗せる）
	syntax string
}

// collect は、root の下を歩き、設定の files: に当たったファイルをパス順に集める。
//
// 1つのファイルが複数の syntax エントリに当たったときは、名前順で最初のものを採る。
// 設定の書き手が重なりを作らない限り起きないが、起きたときに走査の結果が揺れないようにする。
func collect(cfg *config.Config, root string) ([]matched, error) {
	names := slices.Sorted(maps.Keys(cfg.Syntax))

	var out []matched
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// .git の中はソースではない。設定で除外させる筋合いのものではないので、ここで落とす。
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

// matchAny は、glob のいずれかがパスに当たるかを見る（D-9: ** を含む照合は doublestar）。
func matchAny(globs []string, path string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, path); err == nil && ok {
			return true
		}
	}
	return false
}

// auditFile は、ファイル1つを読み、器の違反を Result に足す。
func auditFile(cfg *config.Config, root string, m matched, res *Result) error {
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
	res.Comments += len(comments)

	// mode: content-only は器を問わない。層2（rules）が入るまで、見るものが無い。
	if syn.Mode != "structural" {
		return nil
	}
	for _, c := range comments {
		if _, v := rule.Vessel(c, syn.Allow, cfg.Disposition, spec); v != nil {
			res.Findings = append(res.Findings, Finding{Path: m.path, Syntax: m.syntax, Violation: *v})
		}
	}
	return nil
}
