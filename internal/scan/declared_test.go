package scan

import (
	"slices"
	"testing"
)

// TestScanDeclared は、設定が宣言したコメント記法（DeclaredSpec）で読むことを押さえる。要望の起点は
// 「; で始まる行の中の # から先だけがコメントになり、行の大半が未監査のまま緑に見えた」——複数の
// 行コメント記号を宣言できれば、その行はまるごとコメントとして取り出される。
func TestScanDeclared(t *testing.T) {
	nsis := DeclaredSpec("nsis", []string{";", "#"}, [][2]string{{"/*", "*/"}})

	tests := []struct {
		name string
		spec LangSpec
		src  string
		want []want
	}{
		{
			name: "; で始まる行はまるごとコメント（# から先だけにならない）",
			spec: nsis,
			src:  "; hooks.nsh — installer hooks (#920).\n",
			want: []want{
				{line: 1, endLine: 1, col: 1, kind: KindLine, text: "; hooks.nsh — installer hooks (#920)."},
			},
		},
		{
			name: "宣言した2つ目の記号（#）も行コメントを開く",
			spec: nsis,
			src:  "Section Install\n# 後で消す\n",
			want: []want{
				{line: 2, endLine: 2, col: 1, kind: KindLine, text: "# 後で消す"},
			},
		},
		{
			name: "宣言したブロックの対で読む",
			spec: nsis,
			src:  "/* 説明\n続き */\nName foo  ; 後ろ\n",
			want: []want{
				{line: 1, endLine: 2, col: 1, kind: KindBlock, text: "/* 説明\n続き */"},
				{line: 3, endLine: 3, col: 11, kind: KindLine, text: "; 後ろ"},
			},
		},
		{
			name: "行頭でも空白の直後でもない記号は開かない（語の中の #）",
			spec: nsis,
			src:  "StrCpy $R0 $INSTDIR#tmp\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkComments(t, comments(Scan([]byte(tt.src), tt.spec)), tt.want)
		})
	}
}

// TestDeclaredBody は、宣言した複数の記号を Body / commentOpeners が剥がすことを押さえる。行の頭に
// 立つのが「;」でも「#」でも本文だけになり、ブロックの閉じ（*/）も落ちる。
func TestDeclaredBody(t *testing.T) {
	nsis := DeclaredSpec("nsis", []string{";", "#"}, [][2]string{{"/*", "*/"}})

	tests := []struct {
		text string
		want string
	}{
		{"; TODO: 消す", "TODO: 消す"},
		{"# 内部参照 D-138", "内部参照 D-138"},
		{"/* 説明 */", "説明"},
	}
	for _, tt := range tests {
		if got := Body(tt.text, nsis); got != tt.want {
			t.Errorf("Body(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

// TestDeclaredLongestBlockPair は、接頭辞を共有するブロックの対でも、いちばん長い開きに当てて閉じを
// 取り違えないことを押さえる（宣言に /* と /** が並んでも）。
func TestDeclaredLongestBlockPair(t *testing.T) {
	spec := DeclaredSpec("x", nil, [][2]string{{"/*", "*/"}, {"/**", "**/"}})
	got := comments(Scan([]byte("/** 説明 **/\n"), spec))
	want := []want{{line: 1, endLine: 1, col: 1, kind: KindBlock, text: "/** 説明 **/"}}
	checkComments(t, got, want)

	if body := Body("/** 説明 **/", spec); body != "説明" {
		t.Errorf("Body = %q, want %q", body, "説明")
	}
}

// TestLineMarkersBlockPairs は、単一フィールドと複数フィールドが同じ並びに畳まれることを押さえる
// （組み込みの字句は単一で書き、宣言した字句は複数で書くが、読む側は区別しない）。
func TestLineMarkersBlockPairs(t *testing.T) {
	single := ShellSpec()
	if got := single.lineMarkers(); !slices.Equal(got, []string{"#"}) {
		t.Errorf("single lineMarkers = %v", got)
	}
	if got := single.blockPairs(); got != nil {
		t.Errorf("shell に block は無い: %v", got)
	}

	multi := DeclaredSpec("x", []string{";", "#"}, [][2]string{{"/*", "*/"}})
	if got := multi.lineMarkers(); !slices.Equal(got, []string{";", "#"}) {
		t.Errorf("multi lineMarkers = %v", got)
	}
	if got := multi.blockPairs(); len(got) != 1 || got[0] != [2]string{"/*", "*/"} {
		t.Errorf("multi blockPairs = %v", got)
	}
}
