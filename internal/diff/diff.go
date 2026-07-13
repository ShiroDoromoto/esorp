// Package diff は、git の差分から変更行の集合を得る。check --diff が、
// 変更行に重なるコメントだけを監査対象に絞るために使う。
//
// 比較するのは「<ref> と HEAD の分岐点」と「作業ツリー」。分岐点を基点に取るので、<ref> 側が
// 進んでも他人の変更を拾わない。比較先を作業ツリーに取るので、行番号は check が読むファイルの
// ものと一致する（HEAD と比べると、pre-commit のようにコミット前の状態を検査する場面で行がずれる）。
package diff

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Range は追加された行の範囲。両端を含む1始まりの行番号。
type Range struct {
	From int
	To   int
}

// Ranges は、パス（ツリーの根からの相対パス）ごとの追加行の範囲。
type Ranges map[string][]Range

// Overlaps は、path の from..to 行に追加行が1行でも重なるかを見る。
func (r Ranges) Overlaps(path string, from, to int) bool {
	for _, rg := range r[path] {
		if rg.From <= to && from <= rg.To {
			return true
		}
	}
	return false
}

// Changed は、root を含むリポジトリで、ref との分岐点から作業ツリーまでに追加された行を集める。
// パスは root からの相対パスで返る（root がリポジトリの根でなくてもよく、root の外の変更は落ちる）。
// git diff は追跡していないファイルを出さないので、まだ add していない新しいファイルは別に拾い、
// 丸ごと新しいものとして全行を追加行に数える（拾わないと、新規ファイルが素通りする）。
func Changed(root, ref string) (Ranges, error) {
	base, err := git(root, "merge-base", ref, "HEAD")
	if err != nil {
		if shallow(root) {
			return nil, fmt.Errorf("%s と HEAD の分岐点を取れません（浅いクローンなので、分岐点まで履歴が届いていません。"+
				"actions/checkout なら fetch-depth: 0）: %w", ref, err)
		}
		return nil, fmt.Errorf("%s と HEAD の分岐点を取れません: %w", ref, err)
	}

	out, err := git(root, "-c", "core.quotepath=false", "diff",
		"--unified=0", "--no-color", "--relative", strings.TrimSpace(base))
	if err != nil {
		return nil, fmt.Errorf("差分を取れません: %w", err)
	}
	ranges, err := Parse(strings.NewReader(out))
	if err != nil {
		return nil, err
	}

	untracked, err := git(root, "-c", "core.quotepath=false",
		"ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("追跡していないファイルを取れません: %w", err)
	}
	for _, p := range strings.Split(strings.TrimSpace(untracked), "\n") {
		if p != "" {
			ranges[p] = append(ranges[p], Range{From: 1, To: math.MaxInt})
		}
	}
	return ranges, nil
}

// Parse は git diff --unified=0 の出力から、パスごとの追加行を読む。見るのは「+++ b/<path>」と
// 「@@ -a,b +c,d @@」だけで、追加行の範囲は c..c+d-1（d を省いた形は1行、d が 0 の形＝純粋な
// 削除は追加行を持たない）。削除されたファイルは「+++ /dev/null」で現れ、ディスクに無いので落とす。
// 走査の緩衝を広げてあるのは、長い行を持つ差分で止まらないようにするため。
func Parse(r io.Reader) (Ranges, error) {
	ranges := Ranges{}
	path := ""

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			path = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			if path == "/dev/null" {
				path = ""
			}
		case strings.HasPrefix(line, "@@ ") && path != "":
			rg, ok, err := hunk(line)
			if err != nil {
				return nil, err
			}
			if ok {
				ranges[path] = append(ranges[path], rg)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("差分を読めません: %w", err)
	}
	return ranges, nil
}

// hunk は「@@ -a,b +c,d @@ ...」のハンクヘッダから追加行の範囲を取る。
// 追加行を持たないハンク（d が 0）では ok が false。
func hunk(line string) (Range, bool, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return Range{}, false, fmt.Errorf("ハンクヘッダを読めません: %q", line)
	}

	start, count := strings.TrimPrefix(fields[2], "+"), "1"
	if from, to, ok := strings.Cut(start, ","); ok {
		start, count = from, to
	}

	first, err := strconv.Atoi(start)
	if err != nil {
		return Range{}, false, fmt.Errorf("ハンクヘッダを読めません: %q", line)
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return Range{}, false, fmt.Errorf("ハンクヘッダを読めません: %q", line)
	}
	if n == 0 {
		return Range{}, false, nil
	}
	return Range{From: first, To: first + n - 1}, true, nil
}

// shallow は、root のリポジトリが浅いクローンかを見る。判定そのものが落ちたときは false
// （分岐点を取れない理由の説明に使うだけなので、判定できなければ何も足さない）。
func shallow(root string) bool {
	out, err := git(root, "rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// git は root で git を回し、標準出力を返す。落ちたときは標準エラーをそのままエラーに載せる。
func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}
