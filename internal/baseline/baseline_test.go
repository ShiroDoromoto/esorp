package baseline

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func entry(path, id, body string, occ int) Entry {
	return Entry{Key: Key(path, id, body, occ), Path: path, ID: id}
}

// TestKey は、パス・違反 id・本文・出現順のどれが変わってもキーが変わることを確かめる。行番号は
// 含まない。
func TestKey(t *testing.T) {
	base := Key("a.go", "place-not-allowed", "以前はこうだった。", 0)

	for _, tt := range []struct {
		name string
		key  string
	}{
		{"パスが違えば別のキー", Key("b.go", "place-not-allowed", "以前はこうだった。", 0)},
		{"違反 id が違えば別のキー", Key("a.go", "form-subject", "以前はこうだった。", 0)},
		{"本文が違えば別のキー", Key("a.go", "place-not-allowed", "以前はこうだった", 0)},
		{"出現順が違えば別のキー", Key("a.go", "place-not-allowed", "以前はこうだった。", 1)},
	} {
		if tt.key == base {
			t.Errorf("%s: 同じキーになっている", tt.name)
		}
	}

	if Key("a.go", "place-not-allowed", "以前はこうだった。", 0) != base {
		t.Error("同じ材料から違うキーが出ている")
	}
}

// TestRatchet は、ラチェットが減る方向にしか動かないことと、書き出しがキー順に並ぶこと（差分が
// 読めるように）を確かめる。
func TestRatchet(t *testing.T) {
	old := entry("a.go", "place-not-allowed", "古い違反。", 0)
	fixed := entry("a.go", "place-not-allowed", "直した違反。", 0)
	fresh := entry("b.go", "form-subject", "新しい違反。", 0)

	b := &Baseline{keys: map[string]bool{old.Key: true, fixed.Key: true}}

	got := b.Ratchet([]Entry{old, fresh}, false)
	if len(got) != 1 || got[0].Key != old.Key {
		t.Fatalf("残るのは「今も違反していて baseline にあるもの」だけのはず: %#v", got)
	}

	got = b.Ratchet([]Entry{old, fresh}, true)
	if len(got) != 2 {
		t.Fatalf("--allow-new なら新しい違反も載るはず: %#v", got)
	}
	if !slices.IsSortedFunc(got, func(x, y Entry) int { return strings.Compare(x.Key, y.Key) }) {
		t.Errorf("キーでソートされていない: %#v", got)
	}
}

// TestSaveLoad は、書いて読み戻せることを確かめる。ファイルが無いのはエラーではない。
func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".esorp-baseline.json")

	b, err := Load(path)
	if err != nil {
		t.Fatalf("ファイルが無いのはエラーではないはず: %v", err)
	}
	if b.Len() != 0 {
		t.Fatalf("空のはず: %d", b.Len())
	}

	e := entry("a.go", "form-refs", "#42 で入った。", 0)
	if err := Save(path, []Entry{e}); err != nil {
		t.Fatal(err)
	}

	b, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Has(e.Key) || b.Len() != 1 {
		t.Fatalf("書いたキーが読めない: %d", b.Len())
	}
}

// TestLoadRejectsUnknownVersion は、読めない version を設定エラーとして返すことを確かめる
// （黙って空として扱わない）。
func TestLoadRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".esorp-baseline.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("未知の version を受け入れてしまっている")
	}
}
