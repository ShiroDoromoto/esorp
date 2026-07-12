// Package scan は、ソースを字句に分解し、コメントと文字列リテラルを見分ける。
// 言語ごとの差分（生文字列・ブロックコメントのネスト・doc 記法）は LangSpec が吸収し、
// ファミリ（cstyle など）ごとにスキャナを持つ。
package scan

// Kind はトークンの種別。
//
// コメント以外のトークン（Word / Punct / String）も落とさずに返す。位置クラスの判定は
// 「コメントの前後の非空白トークンが何か」「今どのスコープの中か」を見るため、
// コメントだけを抜き出したトークン列では足りない（→ internal/place）。
type Kind int

const (
	KindLine     Kind = iota // 行コメント（// …）
	KindBlock                // ブロックコメント（/* … */）
	KindDocLine              // doc 専用の行コメント（Rust の /// //!）
	KindDocBlock             // doc 専用のブロックコメント（TS の /** … */）
	KindWord                 // 識別子・キーワード・数値
	KindPunct                // 記号1文字（{ } ( ) など）
	KindString               // 文字列・ルーン・生文字列のリテラル
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
	default:
		return "unknown"
	}
}

// IsComment は、この種別が4種のコメントのいずれかであることを表す。
func (k Kind) IsComment() bool {
	return k == KindLine || k == KindBlock || k == KindDocLine || k == KindDocBlock
}

// Token は1つの字句。Line / Col は 1 始まりで、Col はその行の先頭からのバイト数で数える。
// EndLine は、複数行にまたがるトークン（ブロックコメント・生文字列）の終端行であり、
// 1行で閉じるトークンでは Line と同じになる。
type Token struct {
	Kind    Kind
	Line    int
	Col     int
	EndLine int
	Text    string // ソースに現れたままの生テキスト（コメント記号や引用符を含む）
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
