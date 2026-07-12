package rule

import (
	"strconv"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// テンプレートの既定と同じ器。header / doc / ラベル付きの trailing だけを許し、
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
	var out []*Violation
	for _, c := range place.Classify(scan.CStyle([]byte(src), spec), spec) {
		if _, v := Vessel(c, allows, disposition, spec); v != nil {
			out = append(out, v)
		}
	}
	return out
}

func TestVessel(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string // "id place 行"
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

// kind を絞った器は、その kind のコメントだけを受け入れる。
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

// 同じ place を許す allow が複数あれば、どれか1つが受け入れれば通る。
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

// 器を許した allow を返すこと（書式の検査がその form を使う）。
func TestVesselReturnsMatchedAllow(t *testing.T) {
	spec := scan.GoSpec()
	src := "package p\n\n// Open はストアを開く。\nfunc Open() {}\n"

	comments := place.Classify(scan.CStyle([]byte(src), spec), spec)
	doc := comments[0]

	allows := []config.Allow{{Place: "header"}, {Place: "doc", Form: &config.Form{Subject: "required"}}}
	a, v := Vessel(doc, allows, disposition, spec)
	if v != nil {
		t.Fatalf("違反ではないはず: %#v", v)
	}
	if a == nil || a.Form == nil || a.Form.Subject != "required" {
		t.Fatalf("器を許した allow が返っていない: %#v", a)
	}
}
