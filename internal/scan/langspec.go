package scan

import (
	"path/filepath"
	"strings"
)

// StringSpec は文字列リテラルの1形。Open / Close が同じ引用符でも構わない。
type StringSpec struct {
	Open  string
	Close string

	// Escape は、バックスラッシュが次の1文字を無効にするか。
	Escape bool

	// Multiline は、改行を含められるか。
	Multiline bool

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

	// Here は、開きが行末に立ち、閉じが行頭に立つ形（PowerShell のヒアストリング @" … "@）。
	// 中身は引用符も「#」も含みうるただの文字列で、行頭でない「"@」では閉じない。この2つを見ないと、
	// ハッシュテーブル（@{…}）や配列（@(…)）の中の文字列を開きと読み違え、その先のコメントを飲み込む。
	Here bool
}

// openAt は、src の pos がこの形の開きに当たるなら、開きのバイト数と、それに対応する
// 閉じ記号を返す。閉じ記号が開きに依るのは Hashes のときだけで、他は Close そのもの。
// Here の開きは行末に立たなければならないので、後ろに何か（空白以外）が続くなら開きではない。
func (sp StringSpec) openAt(src []byte, pos int) (n int, close string, ok bool) {
	if sp.Here {
		if !hasAt(src, pos, sp.Open) || !restOfLineBlank(src, pos+len(sp.Open)) {
			return 0, "", false
		}
		return len(sp.Open), sp.Close, true
	}

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

// restOfLineBlank は、src の pos から行末までが空白だけであることを見る（末尾に達していてもよい）。
func restOfLineBlank(src []byte, pos int) bool {
	for i := pos; i < len(src); i++ {
		switch src[i] {
		case ' ', '\t', '\r':
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

// LangSpec は言語差を吸収する。ファミリ（cstyle / hash / sgml / cssblock）が違っても、コメントと
// 文字列を見分けるという仕事は同じであり、違うのは記号と、記号がコメントを開く条件だけ。
// スキャナ（Scan）は1つで、言語もファミリも知らない。
// 字句に関わらない DeclKeywords / TypeLikeOpeners / FuncOpeners / DeclPrefixes も、
// 言語差である以上ここに置く。
// 使うのは位置クラスの判定（internal/place）だが、判定そのものは言語をまたいで同じであり、
// 言語ごとに違うのはこの語彙だけ。
type LangSpec struct {
	Name        string
	LineComment string
	BlockOpen   string
	BlockClose  string

	// BlockNests は、ブロックコメントがネストするか（Rust / Swift はネストする）。
	BlockNests bool

	// LineCommentSpaced は、行コメント記号が、行頭か空白の直後にあるときだけコメントを開くか。
	// 「#」は語の中にも現れる（シェルの ${x#y}、URL の断片）ので、どこに現れてもコメントと読むと、
	// コードを本文として飲み込む。
	LineCommentSpaced bool

	// LineCommentAtLineStart は、行コメント記号が行頭にあるときだけコメントを開くか（gitignore の
	// 「#」は行頭のみ。行中の「#」はパターンの一部）。
	LineCommentAtLineStart bool

	// BlockStars は、ブロックコメントの継ぎ行に「*」を添える流儀か（/* … */ の系統）。添えない流儀
	// （<!-- -->）で剥がすと、箇条書きの「*」が本文から消える。
	BlockStars bool

	// BlockScalars は、ブロックスカラー（YAML の | >）を持つか。その中身はコメント記号を含みうる
	// ただの文字列であり、コードでもない。
	BlockScalars bool

	// Heredocs は、ヒアドキュメント（シェルの <<EOF … EOF）を持つか。ブロックスカラーと同じく、
	// 中身はコメント記号を含みうるただの文字列。
	Heredocs bool

	// DocLine は doc 専用の行コメント記法。Go は持たない。
	DocLine []string

	// DocBlock は doc 専用のブロックコメント記法。Go は持たない。
	DocBlock []string

	// Strings は文字列リテラルの形。長い接頭辞から先に照合する。
	Strings []StringSpec

	// DocInner は、doc 記法のうち、次の宣言ではなく、それを囲むものを説明するもの（Rust の //! /*!）。
	DocInner []string

	// DocFences は、doc が Markdown であり、コードブロックをフェンス（```）で囲むか。Go の doc は
	// Markdown ではなく、コードブロックはタブの字下げで書くので、持たない。
	DocFences bool

	// DeclKeywords は宣言を開始するキーワード。
	DeclKeywords []string

	// TypeLikeOpeners は、型を定義するブロックを開くキーワード。この中の宣言は doc を名乗れる。
	TypeLikeOpeners []string

	// FuncOpeners は、関数本体を開くキーワード。この中では doc を名乗れない。
	FuncOpeners []string

	// GroupOpeners は、宣言を括弧でまとめるブロックを開くキーワード（Go の const ( … )）。
	// 中に並ぶのはキーワードを伴わない宣言なので、型を定義するブロックと同じく doc を名乗れる。
	// 持たない言語もある（Rust / TypeScript は宣言を括弧でまとめない）。
	GroupOpeners []string

	// DeclPrefixes は、宣言の前に置かれる記号（Rust の属性 #[…]）。宣言の一部として扱う。
	DeclPrefixes []string

	// JSX は、タグ（<div> … </div>）の中身をテキストとして読むか。テキストの中の // はコメント
	// ではなく、' は文字列を開かない。
	JSX bool

	// Regex は、正規表現リテラル（/…/g）を持つか。引用符を含む正規表現を文字列の開きと読むと、
	// 行の後ろにあるコメントを飲み込む。
	Regex bool

	// ExprKeywords は、その直後から式が始まりうるキーワード（return / await …）。「/」が除算か
	// 正規表現か、「<」が比較・ジェネリクスか JSX かは、直前のトークンでしか当たりを付けられない。
	ExprKeywords []string
}

// GoSpec は Go の字句。Go には doc 専用記法が無く、doc コメントとは「宣言の直前に置かれた //」の
// ことでしかない。つまり Go では位置を見ないと器を判定できず、DocLine / DocBlock は nil になる。
func GoSpec() LangSpec {
	return LangSpec{
		Name:        "go",
		LineComment: "//",
		BlockOpen:   "/*",
		BlockClose:  "*/",
		BlockNests:  false,
		BlockStars:  true,
		Strings: []StringSpec{
			{Open: `"`, Close: `"`, Escape: true},
			{Open: "'", Close: "'", Escape: true},
			{Open: "`", Close: "`", Multiline: true},
		},
		DeclKeywords:    []string{"func", "type", "var", "const", "package", "import"},
		TypeLikeOpeners: []string{"type", "struct", "interface"},
		FuncOpeners:     []string{"func"},
		GroupOpeners:    []string{"const", "var", "type"},
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
		BlockStars:  true,
		DocLine:     []string{"///", "//!"},
		DocBlock:    []string{"/**", "/*!"},
		DocInner:    []string{"//!", "/*!"},
		DocFences:   true,
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
		BlockStars:  true,
		Regex:       true,
		DocBlock:    []string{"/**"},
		DocFences:   true,
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
		ExprKeywords: []string{
			"return", "yield", "await", "throw", "case", "default",
			"do", "else", "in", "of", "new", "typeof", "void", "delete",
		},
	}
}

// TSXSpec は TSX（.tsx）の字句。TS との差は JSX ひとつで、字句としては「タグの中身はテキストで
// あって、コードでも文字列でもない」ことに尽きる。
func TSXSpec() LangSpec {
	spec := TSSpec()
	spec.Name = "tsx"
	spec.JSX = true
	return spec
}

// JSSpec は JavaScript の字句。TypeScript との差は型の語彙だけで、文字列・正規表現・コメントの
// 読み方は同じ。型の語彙は載せない。type / interface / enum / namespace / declare は JS では
// ただの識別子で、`type = "a"` のような代入を宣言と読むと、その直前のコメントが doc を名乗り、
// 書式の検査が誤爆する。
func JSSpec() LangSpec {
	spec := TSSpec()
	spec.Name = "javascript"
	spec.DeclKeywords = []string{"function", "class", "const", "let", "var", "export", "import"}
	spec.TypeLikeOpeners = []string{"class"}
	return spec
}

// JSXSpec は JSX（.jsx）の字句。JS との差は JSX ひとつ。
// .js に JSX を書く流儀もあるが、拡張子では見分けが付かない。.js は JSX 無しで読む。
func JSXSpec() LangSpec {
	spec := JSSpec()
	spec.Name = "jsx"
	spec.JSX = true
	return spec
}

// CSSSpec は CSS の字句（cssblock ファミリ）。行コメントは持たず、/* */ だけ。16進の色（#fff）に
// 「#」が現れるが、hash ファミリではないので、コメントの開きにはならない。
func CSSSpec() LangSpec {
	return LangSpec{
		Name:       "css",
		BlockOpen:  "/*",
		BlockClose: "*/",
		BlockStars: true,
		Strings: []StringSpec{
			{Open: `"`, Close: `"`, Escape: true},
			{Open: "'", Close: "'", Escape: true},
		},
	}
}

// SGMLSpec は SGML 系（HTML / SVG / Markdown）の字句。コメントは <!-- --> だけで、その外側は
// タグでも散文でもコメントではない。文字列リテラルを持たないのは、属性値の引用符とMarkdown の
// 散文の引用符が同じ形をしており、散文の「'」を文字列の開きと読むと、その先のコメントを飲み込む
// ため。属性値の中に「-->」が現れることは、まず無い。
func SGMLSpec() LangSpec {
	return LangSpec{
		Name:       "sgml",
		BlockOpen:  "<!--",
		BlockClose: "-->",
	}
}

// ShellSpec はシェルの字句（hash ファミリ）。「#」がコメントを開くのは行頭か空白の直後だけで、
// 語の中の「#」（${x#y}）はコメントではない。引用符は行をまたげることにしない。またげることにして
// 閉じ忘れを飲み込むと、その先にある本物のコメントが文字列の中に消え、検査されないまま通る。
// 迷ったら違反にする側へ倒す。
func ShellSpec() LangSpec {
	return LangSpec{
		Name:              "shell",
		LineComment:       "#",
		LineCommentSpaced: true,
		Heredocs:          true,
		Strings: []StringSpec{
			{Open: `"`, Close: `"`, Escape: true},
			{Open: "'", Close: "'"},
		},
	}
}

// YAMLSpec は YAML の字句（hash ファミリ）。長い文字列はブロックスカラー（| >）で書くもので、
// ヒアドキュメントは持たない（「<<」は併合キー）。
func YAMLSpec() LangSpec {
	spec := ShellSpec()
	spec.Name = "yaml"
	spec.BlockScalars = true
	spec.Heredocs = false
	return spec
}

// TOMLSpec は TOML の字句（hash ファミリ）。
func TOMLSpec() LangSpec {
	spec := ShellSpec()
	spec.Name = "toml"
	spec.Heredocs = false
	return spec
}

// MakeSpec は Makefile の字句（hash ファミリ）。レシピ行（タブ以降）はシェルだが、「#」がコメントを
// 開く条件はシェルでも同じなので、レシピ行を分けて読む必要はない。
func MakeSpec() LangSpec {
	spec := ShellSpec()
	spec.Name = "make"
	return spec
}

// DockerSpec は Dockerfile の字句（hash ファミリ）。
func DockerSpec() LangSpec {
	spec := ShellSpec()
	spec.Name = "dockerfile"
	return spec
}

// GitignoreSpec は gitignore の字句（hash ファミリ）。「#」は行頭のみコメントで、行中の「#」は
// パターンの一部。引用符も持たない（行はまるごとパターン）。
func GitignoreSpec() LangSpec {
	return LangSpec{
		Name:                   "gitignore",
		LineComment:            "#",
		LineCommentAtLineStart: true,
	}
}

// PowerShellSpec は PowerShell の字句（hash ファミリ）。「#」に加えてブロックコメント <# #> を持つ。
// 長い文字列はヒアストリング（@" … "@）で書き、シェルのヒアドキュメント（<<EOF）は持たない。
// ヒアストリングは開きが長いので、普通の引用符より先に照合する。
func PowerShellSpec() LangSpec {
	spec := ShellSpec()
	spec.Name = "powershell"
	spec.BlockOpen = "<#"
	spec.BlockClose = "#>"
	spec.Heredocs = false
	spec.Strings = append([]StringSpec{
		{Open: `@"`, Close: `"@`, Multiline: true, Here: true},
		{Open: "@'", Close: "'@", Multiline: true, Here: true},
	}, spec.Strings...)
	return spec
}

// SpecFor は、ファイルの名前からその言語の字句を選ぶ。拡張子だけでは足りないのは、拡張子を持たない
// ファイル（Makefile / Dockerfile / .gitignore）があるため。設定の files: は glob なので、字句を
// 持たない拡張子をファミリに並べることもできる。持っていないことを黙って飲み込むと、そのファイルは
// 検査されないまま適合したように見えるので、引けなかったことを呼び手に返し、呼び手が告げる。
func SpecFor(path string) (LangSpec, bool) {
	switch filepath.Base(path) {
	case "Makefile", "makefile", "GNUmakefile":
		return MakeSpec(), true
	case "Dockerfile":
		return DockerSpec(), true
	case ".gitignore", ".dockerignore":
		return GitignoreSpec(), true
	}

	switch filepath.Ext(path) {
	case ".go":
		return GoSpec(), true
	case ".rs":
		return RustSpec(), true
	case ".ts", ".mts", ".cts":
		return TSSpec(), true
	case ".tsx":
		return TSXSpec(), true
	case ".js", ".mjs", ".cjs":
		return JSSpec(), true
	case ".jsx":
		return JSXSpec(), true
	case ".css":
		return CSSSpec(), true
	case ".html", ".htm", ".svg", ".xml", ".md":
		return SGMLSpec(), true
	case ".sh", ".bash", ".zsh":
		return ShellSpec(), true
	case ".mk":
		return MakeSpec(), true
	case ".yml", ".yaml":
		return YAMLSpec(), true
	case ".toml":
		return TOMLSpec(), true
	case ".ps1", ".psm1":
		return PowerShellSpec(), true
	}
	return LangSpec{}, false
}

// FamilySpec は、構文ファミリの既定の字句。名前からも拡張子からも引けないファイル
// （.githooks/pre-commit）を、設定が files: で当てたエントリのファミリから読むためのもの。
// cstyle が既定を持たないのは、コメント記号は共通でも、器の判定が言語ごとの宣言の語彙に依るため。
// 取り違えた語彙で位置クラスを決めると、doc でないコメントが doc を名乗り、書式の検査が誤爆する。
func FamilySpec(family string) (LangSpec, bool) {
	switch family {
	case "hash":
		return ShellSpec(), true
	case "sgml":
		return SGMLSpec(), true
	case "cssblock":
		return CSSSpec(), true
	}
	return LangSpec{}, false
}
