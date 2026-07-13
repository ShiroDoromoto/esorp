// Package scan は、ソースを字句に分解し、コメントと文字列リテラルを見分ける。
// スキャナは1つで、言語ごとの差分（生文字列・ブロックコメントのネスト・doc 記法）も
// ファミリ（cstyle / hash / sgml / cssblock）の差も、すべて LangSpec が吸収する。
package scan

// Kind はトークンの種別。コメント以外のトークン（Word / Punct / String）も落とさずに返すのは、
// 位置クラスの判定が「コメントの前後の非空白トークンが何か」「今どのスコープの中か」を見るため
// （コメントだけを抜き出したトークン列では足りない → internal/place）。
type Kind int

const (
	// KindLine は行コメント（// …）。
	KindLine Kind = iota

	// KindBlock はブロックコメント（/* … */）。
	KindBlock

	// KindDocLine は doc 専用の行コメント（Rust の /// //!）。
	KindDocLine

	// KindDocBlock は doc 専用のブロックコメント（TS の /** … */）。
	KindDocBlock

	// KindWord は識別子・キーワード・数値。
	KindWord

	// KindPunct は記号1文字（{ } ( ) など）。
	KindPunct

	// KindString は、中身をコードとして読まないもの（文字列・ルーン・生文字列のリテラル、
	// 正規表現リテラル、JSX のテキスト）。
	KindString

	// KindShebang は1行目の「#!…」。コメントでもコードでもない、カーネルへの指示。
	KindShebang
)

func (k Kind) String() string {
	switch k {
	case KindLine:
		return "line"
	case KindBlock:
		return "block"
	case KindDocLine:
		return "docline"
	case KindDocBlock:
		return "docblock"
	case KindWord:
		return "word"
	case KindPunct:
		return "punct"
	case KindString:
		return "string"
	case KindShebang:
		return "shebang"
	default:
		return "unknown"
	}
}

// IsComment は、この種別が4種のコメントのいずれかであることを表す。
func (k Kind) IsComment() bool {
	return k == KindLine || k == KindBlock || k == KindDocLine || k == KindDocBlock
}

// IsCode は、この種別がコードの一部であることを表す。コメントと shebang だけがコードではない。
// shebang をコードとして数えると、その直後に置かれたファイル冒頭のコメントが header を名乗れなく
// なる（前にコードがあることになる）ので、前後のトークンを見るときは、無かったものとして飛ばす。
func (k Kind) IsCode() bool {
	return !k.IsComment() && k != KindShebang
}

// Token は1つの字句。Line / Col は 1 始まりで、Col はその行の先頭からのバイト数で数える。
// EndLine は、複数行にまたがるトークン（ブロックコメント・生文字列）の終端行であり、
// 1行で閉じるトークンでは Line と同じになる。
type Token struct {
	Kind    Kind
	Line    int
	Col     int
	EndLine int

	// Text は、ソースに現れたままの生テキスト（コメント記号や引用符を含む）。
	Text string
}

// ParseKind は、設定に書かれたコメント種別の名前を値にする。コメントの4種だけを引ける。
func ParseKind(s string) (Kind, bool) {
	for _, k := range []Kind{KindLine, KindBlock, KindDocLine, KindDocBlock} {
		if k.String() == s {
			return k, true
		}
	}
	return 0, false
}
