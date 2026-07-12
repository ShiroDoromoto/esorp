// Package place は、コメントの位置クラス（doc / leading / trailing / inside …）を判定する。
// スコープスタックを持ち、doc が紐づく宣言名を取り出して持ち回る（書式の subject が使う）。
// 字句と違い、この判定は言語をまたいで同じであり、scan とは混ぜない。
package place
