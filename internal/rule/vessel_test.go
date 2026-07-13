package rule

import (
	"strconv"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// templateAllows は、テンプレートの既定と同じ器。header / doc / ラベル付きの trailing だけを許し、
// leading と orphan は許さない。
func templateAllows() []config.Allow {
	return []config.Allow{
		{Place: "header"},
		{Place: "doc"},
		{Place: "trailing", Label: []string{"SAFETY:", "TODO:", "nolint:"}},
	}
}

var disposition = map[string]string{
	PlaceNotAllowed: "この位置のコメントは許可されていません。",
	LabelRequired:   "この位置のコメントにはラベルが必要です。",
}

// vessel は、Go のソース断片を検査して違反だけを返す。
func vessel(t *testing.T, src string, allows []config.Allow) []*Violation {
	t.Helper()

	spec := scan.GoSpec()
	target := Target{Syntax: "cstyle", Path: "a.go"}

	var out []*Violation
	for _, c := range place.Classify(scan.CStyle([]byte(src), spec), spec) {
		if _, v := Vessel(c, allows, disposition, target, spec); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func TestVessel(t *testing.T) {
	tests := []struct {
		name string
		src  string

		// want は「id place 行」の形で期待する違反。
		want []string
	}{
		{
			name: "許可された器（header / doc / ラベル付き trailing）は通る",
			src: "// Package p は何かをする。\n" +
				"package p\n" +
				"\n" +
				"// Open はストアを開く。\n" +
				"func Open() error {\n" +
				"\treturn nil // TODO: 実装する\n" +
				"}\n",
			want: nil,
		},
		{
			name: "関数の中の自由コメント（leading）は、中身が何であれ違反",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\t// 以前はここで前方移行していた。\n" +
				"\tg()\n" +
				"}\n",
			want: []string{"place-not-allowed leading 4"},
		},
		{
			name: "浮いたコメント（orphan）も違反",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\tg()\n" +
				"\t// もう呼ばない。\n" +
				"}\n",
			want: []string{"place-not-allowed orphan 5"},
		},
		{
			name: "ラベルの無い行末コメントは label-required",
			src: "package p\n" +
				"\n" +
				"var x = 1 // 適当なメモ\n",
			want: []string{"label-required trailing 3"},
		},
		{
			name: "未知のラベルも label-required（列挙されたものだけが通る）",
			src: "package p\n" +
				"\n" +
				"var x = 1 // FIXME: あとで\n",
			want: []string{"label-required trailing 3"},
		},
		{
			name: "ブロックコメントでもラベルを剥がして見る",
			src: "package p\n" +
				"\n" +
				"var x = 1 /* SAFETY: 呼び出し側が保証する */\n",
			want: nil,
		},
		{
			name: "違反は器ごとに1件（連続する複数行コメントは1つの器）",
			src: "package p\n" +
				"\n" +
				"func f() {\n" +
				"\t// 1行目。\n" +
				"\t// 2行目。\n" +
				"\t// 3行目。\n" +
				"\tg()\n" +
				"}\n",
			want: []string{"place-not-allowed leading 4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vessel(t, tt.src, templateAllows())
			if len(got) != len(tt.want) {
				t.Fatalf("違反 %d 件, want %d 件\n得たもの: %#v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if got := g.ID + " " + g.Place.String() + " " + strconv.Itoa(g.Line); got != w {
					t.Errorf("violation[%d] = %q, want %q", i, got, w)
				}
				if g.Message == "" {
					t.Errorf("violation[%d]: disposition のメッセージが引かれていない", i)
				}
			}
		})
	}
}

// TestVesselKindNarrowing は、kind を絞った器がその kind のコメントだけを受け入れることを確かめる。
func TestVesselKindNarrowing(t *testing.T) {
	allows := []config.Allow{
		{Place: "doc", Kind: []string{"line"}},
	}
	src := "package p\n" +
		"\n" +
		"/* ブロックの doc */\n" +
		"func Open() {}\n" +
		"\n" +
		"// 行の doc\n" +
		"func Close() {}\n"

	got := vessel(t, src, allows)
	if len(got) != 1 || got[0].ID != PlaceNotAllowed || got[0].Line != 3 {
		t.Fatalf("kind: [line] の doc なのでブロックだけが落ちるはず: %#v", got)
	}
}

// TestVesselMultipleAllowsForSamePlace は、同じ place を許す allow が複数あれば、どれか1つが
// 受け入れれば通ることを確かめる。
func TestVesselMultipleAllowsForSamePlace(t *testing.T) {
	allows := []config.Allow{
		{Place: "trailing", Kind: []string{"line"}, Label: []string{"TODO:"}},
		{Place: "trailing", Kind: []string{"block"}},
	}
	src := "package p\n" +
		"\n" +
		"var x = 1 /* ラベル無しでも block なら通る */\n" +
		"var y = 2 // ラベル無しの line は落ちる\n"

	got := vessel(t, src, allows)
	if len(got) != 1 || got[0].ID != LabelRequired || got[0].Line != 4 {
		t.Fatalf("block は通り line だけが落ちるはず: %#v", got)
	}
}

// TestVesselReturnsMatchedAllow は、器を許した allow の添字を返すことを確かめる（書式の検査が
// その form を使う）。
func TestVesselReturnsMatchedAllow(t *testing.T) {
	spec := scan.GoSpec()
	src := "package p\n\n// Open はストアを開く。\nfunc Open() {}\n"

	comments := place.Classify(scan.CStyle([]byte(src), spec), spec)
	doc := comments[0]

	allows := []config.Allow{{Place: "header"}, {Place: "doc", Form: &config.Form{Subject: "required"}}}
	i, v := Vessel(doc, allows, disposition, Target{Syntax: "cstyle", Path: "a.go"}, spec)
	if v != nil {
		t.Fatalf("違反ではないはず: %#v", v)
	}
	if i != 1 {
		t.Fatalf("器を許した allow の添字が違う: %d", i)
	}
}

// TestVesselSite は、層1 の違反が「それを決めた設定の場所」を指すことを確かめる。違反 id と設定は
// 一対一で対応しており、explain はこれを辿って設定の該当箇所を見せる。
func TestVesselSite(t *testing.T) {
	allows := []config.Allow{{Place: "header"}, {Place: "trailing", Label: []string{"TODO:"}}}
	src := "package p\n\nfunc F() {\n\t// 文の直前。\n\tx := 1 // ラベルが無い。\n\t_ = x\n}\n"

	got := vessel(t, src, allows)
	if len(got) != 2 {
		t.Fatalf("違反が %d 件（2 件のはず）: %#v", len(got), got)
	}
	if s := got[0].Site; s.Path != "syntax.cstyle.allow" || s.Allow != -1 || s.Rule != -1 {
		t.Errorf("place-not-allowed が器の列挙を指していない: %#v", s)
	}
	if s := got[1].Site; s.Path != "syntax.cstyle.allow[1].label" || s.Allow != 1 || s.Rule != -1 {
		t.Errorf("label-required がラベルの列挙を指していない: %#v", s)
	}
}
