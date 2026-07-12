package scan

import (
	"path/filepath"
	"strings"
)

// StringSpec は文字列リテラルの1形。Open / Close が同じ引用符でも構わない。
type StringSpec struct {
	Open      string
	Close     string
	Escape    bool // バックスラッシュで次の1文字を無効化するか
	Multiline bool // 改行を含められるか

	// Hashes は、開きの引用符の直前に「#」が任意個入り、閉じにも同じ数の「#」が続く形
	// （Rust の r#"…"#）。区切りの長さが可変なので、Open / Close だけでは表せない。
	// Open は引用符まで含めて書き（r"）、「#」はその引用符の直前に入る。
	Hashes bool

	// OneRune は、中身が1文字（またはエスケープ列1つ）でなければ、この形ではないこと。
	// Rust のライフタイム 'a は文字リテラルではなく、これを取り違えると同じ行の
	// 後ろにあるコメントを文字列の中に飲み込む。
	OneRune bool

	// Interp は、文字列の中でコードに戻る記法の開き（TS のテンプレートリテラルの「${」）。
	// 中は再びコードなので、そこに現れるコメントはコメントとして読む。
	Interp string
}

// openAt は、src の pos がこの形の開きに当たるなら、開きのバイト数と、それに対応する
// 閉じ記号を返す。閉じ記号が開きに依るのは Hashes のときだけで、他は Close そのもの。
func (sp StringSpec) openAt(src []byte, pos int) (n int, close string, ok bool) {
	if !sp.Hashes {
		if hasAt(src, pos, sp.Open) {
			return len(sp.Open), sp.Close, true
		}
		return 0, "", false
	}

	prefix, quote := sp.Open[:len(sp.Open)-1], sp.Open[len(sp.Open)-1:]
	if !hasAt(src, pos, prefix) {
		return 0, "", false
	}
	i := pos + len(prefix)
	for i < len(src) && src[i] == '#' {
		i++
	}
	if !hasAt(src, i, quote) {
		return 0, "", false
	}
	hashes := strings.Repeat("#", i-pos-len(prefix))
	return i + len(quote) - pos, sp.Close + hashes, true
}

// LangSpec は cstyle ファミリの中の言語差を吸収する。
// ファミリが同じでも文字列リテラルの文法は言語ごとに違い、そこを取り違えると
// 文字列の中の // をコメントと誤検知する。
// 字句に関わらない DeclKeywords / TypeLikeOpeners / FuncOpeners / DeclPrefixes も、
// 言語差である以上ここに置く。
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

	// DocInner は、doc 記法のうち、次の宣言ではなく、それを囲むものを説明するもの（Rust の //! /*!）。
	DocInner []string

	DeclKeywords    []string // 宣言を開始するキーワード
	TypeLikeOpeners []string // 型を定義するブロックを開くキーワード（この中の宣言は doc を名乗れる）
	FuncOpeners     []string // 関数本体を開くキーワード（この中では doc を名乗れない）

	// DeclPrefixes は、宣言の前に置かれる記号（Rust の属性 #[…]）。宣言の一部として扱う。
	DeclPrefixes []string
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

// RustSpec は Rust の字句。Go との差はブロックコメントのネスト・可変長の生文字列（r#"…"#）・
// doc 専用記法（/// //! /** /*!）・文字リテラルとライフタイムの区別の4つで、いずれも
// LangSpec に収まる（スキャナ本体は言語を知らない）。
func RustSpec() LangSpec {
	return LangSpec{
		Name:        "rust",
		LineComment: "//",
		BlockOpen:   "/*",
		BlockClose:  "*/",
		BlockNests:  true,
		DocLine:     []string{"///", "//!"},
		DocBlock:    []string{"/**", "/*!"},
		DocInner:    []string{"//!", "/*!"},
		Strings: []StringSpec{
			{Open: `br"`, Close: `"`, Multiline: true, Hashes: true},
			{Open: `r"`, Close: `"`, Multiline: true, Hashes: true},
			{Open: `b"`, Close: `"`, Escape: true},
			{Open: `"`, Close: `"`, Escape: true, Multiline: true},
			{Open: "'", Close: "'", Escape: true, OneRune: true},
		},
		DeclKeywords:    []string{"fn", "struct", "enum", "trait", "impl", "mod", "type", "const", "static", "use", "pub"},
		TypeLikeOpeners: []string{"struct", "enum", "trait", "impl", "mod"},
		FuncOpeners:     []string{"fn"},
		DeclPrefixes:    []string{"#"},
	}
}

// TSSpec は TypeScript の字句。doc 専用記法は JSDoc（/** … */）だけで、行の doc 記法は無い。
// テンプレートリテラルの ${ … } の中は再びコードなので、StringSpec.Interp で受ける。
func TSSpec() LangSpec {
	return LangSpec{
		Name:        "typescript",
		LineComment: "//",
		BlockOpen:   "/*",
		BlockClose:  "*/",
		BlockNests:  false,
		DocBlock:    []string{"/**"},
		Strings: []StringSpec{
			{Open: `"`, Close: `"`, Escape: true},
			{Open: "'", Close: "'", Escape: true},
			{Open: "`", Close: "`", Escape: true, Multiline: true, Interp: "${"},
		},
		DeclKeywords: []string{
			"function", "class", "interface", "enum", "type",
			"const", "let", "var", "export", "import", "declare", "namespace",
		},
		TypeLikeOpeners: []string{"class", "interface", "enum", "namespace"},
		FuncOpeners:     []string{"function"},
		DeclPrefixes:    []string{"@"},
	}
}

// SpecFor は、ファイルの拡張子からその言語の字句を選ぶ。設定の files: は glob なので、
// cstyle ファミリに .tsx を並べることはできるが、その字句をまだ持っていない。持っていないことを
// 黙って飲み込むと、そのファイルは検査されないまま適合したように見えるので、引けなかったことを
// 呼び手に返し、呼び手が告げる。
func SpecFor(path string) (LangSpec, bool) {
	switch filepath.Ext(path) {
	case ".go":
		return GoSpec(), true
	case ".rs":
		return RustSpec(), true
	case ".ts", ".mts", ".cts":
		return TSSpec(), true
	}
	return LangSpec{}, false
}
