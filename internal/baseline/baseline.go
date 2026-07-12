// Package baseline は、既存違反のスナップショットを読み書きする。
//
// これが無いと、大きなコードベースに入れた初回に数千件が光り、全部直すまで CI が赤くなる。
// 誰も導入できない。既存の違反を一覧として抱えたまま、新しい違反だけを止められるようにする。
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Version は .esorp-baseline.json の形。読み手が形の変化に気づけるように持つ。
const Version = 1

// Entry は baseline の1行。照合に使うのは Key だけで、Path と ID は人間が読むための冗長情報。
type Entry struct {
	Key  string `json:"key"`
	Path string `json:"path"`
	ID   string `json:"id"`
}

// Baseline は、読み込んだスナップショット。
type Baseline struct {
	keys map[string]bool
}

// Key は、違反1件を指すキーを組む。
//
// 行番号を使わない。行番号で記録すると、無関係な編集で行がずれるたびに baseline が壊れる。
// body は正規化した本文（scan.Body）で、occurrence は同じキーが同じファイルに複数回現れるときの
// 出現順（0 始まり）。
//
// 帰結として、baseline に載っているコメントの本文を編集するとキーが変わり、違反として現れる。
// これは意図している。触ったなら、あなたがそのコメントの持ち主になる。
func Key(path, id, body string, occurrence int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{path, id, body, strconv.Itoa(occurrence)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Load は baseline を読む。ファイルが無いのはエラーではない（まだ1件も載せていないだけ）。
func Load(path string) (*Baseline, error) {
	b := &Baseline{keys: map[string]bool{}}
	if path == "" {
		return b, nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", path, err)
	}

	var file struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s を読めません: %w", path, err)
	}
	if file.Version != Version {
		return nil, fmt.Errorf("%s: version %d は読めません（このツールが読むのは %d）", path, file.Version, Version)
	}
	for _, e := range file.Entries {
		b.keys[e.Key] = true
	}
	return b, nil
}

// Has は、そのキーが baseline に載っているかを見る。
func (b *Baseline) Has(key string) bool {
	return b.keys[key]
}

// Len は、載っている件数。
func (b *Baseline) Len() int {
	return len(b.keys)
}

// Ratchet は、次に書き出す baseline を決める。**減る方向にしか動かない。**
//
// もう違反していないキーは落ち（current に無い）、新しい違反は載らない（b に無い）。
// 新しい違反を載せるには allowNew を明示する。CI では絶対に使わない。
func (b *Baseline) Ratchet(current []Entry, allowNew bool) []Entry {
	out := []Entry{}
	for _, e := range current {
		if allowNew || b.Has(e.Key) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Key, b.Key) })
	return out
}

// Save は baseline を書き出す。キーでソートした JSON にして、差分が読めるようにする。
func Save(path string, entries []Entry) error {
	data, err := json.MarshalIndent(struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}{Version: Version, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("%s を書けません: %w", path, err)
	}
	return nil
}
