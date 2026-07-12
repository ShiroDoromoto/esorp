package scan

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
type LangSpec struct {
	Name        string
	LineComment string
	BlockOpen   string
	BlockClose  string
	BlockNests  bool         // Rust / Swift はブロックコメントがネストする
	DocLine     []string     // doc 専用の行コメント記法。Go は持たない
	DocBlock    []string     // doc 専用のブロックコメント記法。Go は持たない
	Strings     []StringSpec // 長い接頭辞から先に照合する
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
	}
}
