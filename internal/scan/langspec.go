package scan

import "path/filepath"

// StringSpec は文字列リテラルの1形。Open / Close が同じ引用符でも構わない。
//
// Rust の r#"…"# のように区切りの長さが可変のものは、この形では表せない。
// Rust を載せるときに、この LangSpec を拡張して受け止める。
type StringSpec struct {
	Open      string
	Close     string
	Escape    bool // バックスラッシュで次の1文字を無効化するか
	Multiline bool // 改行を含められるか
}

// LangSpec は cstyle ファミリの中の言語差を吸収する。
// ファミリが同じでも文字列リテラルの文法は言語ごとに違い、そこを取り違えると
// 文字列の中の // をコメントと誤検知する。
// 字句に関わらない DeclKeywords / TypeLikeOpeners / FuncOpeners も、言語差である以上ここに置く。
// 使うのは位置クラスの判定（internal/place）だが、判定そのものは言語をまたいで同じであり、
// 言語ごとに違うのはこの語彙だけ。
type LangSpec struct {
	Name        string
	LineComment string
	BlockOpen   string
	BlockClose  string
	BlockNests  bool         // Rust / Swift はブロックコメントがネストする
	DocLine     []string     // doc 専用の行コメント記法。Go は持たない
	DocBlock    []string     // doc 専用のブロックコメント記法。Go は持たない
	Strings     []StringSpec // 長い接頭辞から先に照合する

	DeclKeywords    []string // 宣言を開始するキーワード
	TypeLikeOpeners []string // 型を定義するブロックを開くキーワード（この中の宣言は doc を名乗れる）
	FuncOpeners     []string // 関数本体を開くキーワード（この中では doc を名乗れない）
}

// GoSpec は Go の字句。
//
// Go には doc 専用記法が無く、doc コメントとは「宣言の直前に置かれた //」のことでしかない。
// つまり Go では位置を見ないと器を判定できず、DocLine / DocBlock は nil になる。
func GoSpec() LangSpec {
	return LangSpec{
		Name:        "go",
		LineComment: "//",
		BlockOpen:   "/*",
		BlockClose:  "*/",
		BlockNests:  false,
		Strings: []StringSpec{
			{Open: `"`, Close: `"`, Escape: true},
			{Open: "'", Close: "'", Escape: true},
			{Open: "`", Close: "`", Multiline: true}, // 生文字列: エスケープ無し・改行可
		},
		DeclKeywords:    []string{"func", "type", "var", "const", "package", "import"},
		TypeLikeOpeners: []string{"type", "struct", "interface"},
		FuncOpeners:     []string{"func"},
	}
}

// SpecFor は、ファイルの拡張子からその言語の字句を選ぶ。
//
// 設定の files: は glob なので、cstyle ファミリに .rs や .ts を並べることはできるが、
// その字句をまだ持っていない。持っていないことを黙って飲み込むと、そのファイルは
// 検査されないまま適合したように見える。引けなかったことを呼び手に返し、呼び手が告げる。
func SpecFor(path string) (LangSpec, bool) {
	switch filepath.Ext(path) {
	case ".go":
		return GoSpec(), true
	}
	return LangSpec{}, false
}
