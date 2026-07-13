package config

// Template は esorp init が生成する設定。既定は実行時のフォールバックではなく、ここにだけ形として
// 存在する。生成された時点でユーザーのものになり、ツールを更新しても勝手には変わらない。言語ごとに
// syntax エントリを分けてあるのは、書式の subject（1行目が宣言の名前で始まること）が Go の doc
// 規約であって、Rust / TypeScript の規約ではないため。
const Template = `# esorp.yaml — コメントの置き場所と書式の宣言。
# ここに書かれているものが、動いているものの全部。隠れた既定は無い。
# 使わない言語のエントリは削ってください。

syntax:
  cstyle-go:
    family: cstyle
    files:
      - "**/*.go"

      # 「!」始まりは除外。他人のコードは自分のコードとして扱わない。
      - "!vendor/**"
    mode: structural

    # 許可する「器」。ここに列挙されていない器のコメントは、中身が何であれ違反。
    allow:
      - place: header               # ファイル冒頭（ライセンス等）

      - place: doc                  # 宣言に紐づく説明
        # 器の中の「書式」。形だけを見る。語彙は見ない。
        form:
          subject: required         # 1行目が、紐づく宣言の名前で始まること（Go の doc 規約）
          headings: deny            # 見出しを書けない（履歴は見出しを付けて書かれる）
          paragraphs: 1             # 段落は1つ（背景を段落として付け足せない）
          refs: deny                # #123 形式の追跡番号への参照を書けない

      - place: trailing             # 行末。ラベル必須
        label: ["SAFETY:", "TODO:", "nolint:"]

      # place: leading  — 許可しない（文の直前の自由コメント）
      # place: orphan   — 許可しない（どこにも紐づかない浮いたコメント）
      #
      # 履歴・事情・作業メモが流れ込むのはこの2つ。許可しなければ、語彙を一切見ずに、
      # それらは構造的に書けなくなる。

  cstyle-rust:
    family: cstyle
    files:
      - "**/*.rs"
    mode: structural
    allow:
      - place: header

      - place: doc
        form:
          # subject は Go の doc 規約。Rust には無いので求めない。
          headings: deny
          paragraphs: 1
          refs: deny

      - place: trailing
        label: ["SAFETY:", "TODO:"]

  cstyle-ts:
    family: cstyle
    files:
      - "**/*.ts"
      - "**/*.mts"
      - "**/*.cts"
      - "**/*.tsx"

      # 「!」始まりは除外。他人のコードは自分のコードとして扱わない。
      - "!**/node_modules/**"
      - "!**/*.d.ts"
    mode: structural
    allow:
      - place: header

      - place: doc
        form:
          headings: deny
          paragraphs: 1
          refs: deny

      - place: trailing
        label: ["TODO:"]

  cstyle-js:
    family: cstyle
    files:
      - "**/*.js"
      - "**/*.mjs"
      - "**/*.cjs"
      - "**/*.jsx"

      - "!**/node_modules/**"
      - "!**/*.min.js"
    mode: structural
    allow:
      - place: header

      - place: doc
        form:
          headings: deny
          paragraphs: 1
          refs: deny

      - place: trailing
        label: ["TODO:"]

  # ここから下は「器の概念が無いファイル」。コメントの種類が1つしかなく、doc も紐づく宣言も無いので、
  # 「許可する器を列挙して残りを落とす」が選択の軸として働かない。層1 を当てず、層2（語彙）だけを当てる。
  # 層2 は既定を持たないので、下の rules: が空のうちは、これらのファイルでは何も起きない。
  # それでも書いておくのは、履歴や追跡番号がコードだけに現れるわけではないため（実測では、追跡番号への
  # 参照は .yml .toml .sh Makefile .gitignore にも遍在していた）。
  hash:
    files:
      - "**/*.yml"
      - "**/*.yaml"
      - "**/*.toml"
      - "**/*.sh"
      - "**/*.ps1"
      - "Makefile"
      - "Dockerfile"
      - "**/.gitignore"
    mode: content-only

  sgml:
    files:
      - "**/*.md"
      - "**/*.html"
      - "**/*.svg"
    mode: content-only

  cssblock:
    files:
      - "**/*.css"
    mode: content-only

# 違反時に提示する始末のしかた。既定は削除（履歴はバージョン管理が持っている）。
# 残す価値のある判断だけ、行き先を文字列で指定できる。ツールは行き先を規定しない。
disposition:
  place-not-allowed: |
    この位置のコメントは許可されていません。
    目の前のコードの説明なら、宣言に紐づく doc コメントに移してください。
    変更の履歴なら、バージョン管理が保持しているので削除してください。
  label-required: |
    この位置のコメントにはラベルが必要です。
  form-subject: |
    doc コメントは、その宣言の説明です。宣言の名前で始めてください。
    名前で始められない内容なら、それは宣言の説明ではありません。
  form-headings: |
    doc コメントに見出しは書けません。
  form-paragraphs: |
    doc コメントの段落は1つです。付け足された段落は、多くの場合、目の前のコードの説明ではありません。
  form-refs: |
    追跡番号への参照です。読み手（将来の参加者・外部の読者・次のエージェント）は追跡システムを
    辿れません。削除してください。

# git が「自分のコードではない」と宣言しているものを、esorp も自分のコードとして扱わない。
# gitignore を黙って見にいくのは設定に見えない挙動になるので、方針としてここに書く。
respect_gitignore: true

# 層2（語彙）。既定は空。
# 文字列マッチは必ず誤検知するため、ツールは既定を持たない。プロジェクトが自分で足す。
#
# 足すなら、「変化を指す専用句」だけにしてください（no longer / used to / かつて / 従来）。
# 実在のリポジトリ1件（285ファイル / 13,186コメント）で測ったところ、この種の句はほぼ全部が
# 本当に履歴でしたが、「時間や新旧を指す汎用語」（old / before / legacy）はほぼ全部が偽陽性でした。
# 誤検知するガードは例外指定を誘発し、やがてツールごと無視されます。
#
# 検査するのは、層1（器と書式）を通ったコメントの本文だけです。本文は折り返しを畳んでから
# （1段落 = 1行）当てるので、句が行をまたいでいても当たります。「no\n// longer」は「no longer」、
# 「かつ\n// て」は「かつて」に戻ります。(?m) を付けたときの ^ と $ が指すのは、
# 折り返された行ではなく段落の端です。
#
# rules:
#   - id: no-history
#     pattern: "no longer|used to|かつて|従来"
#     message: |
#       変化を語っています。今のコードが何であるかだけを書いてください。
#     where:
#       syntax: [cstyle-go]     # 省略時は全エントリ
#       kind: [line, block]     # 省略時は全 kind
#       path: ["**/*.go", "!internal/legacy/**"]   # 省略時は全ファイル。「!」始まりは除外
rules: []

# 層3（意味）。esorp はコメントの意味を判定しない。LLM も呼ばない。
#
# 層1（器と書式）と層2（語彙）を通り抜けたコメントを、変更分（esorp check --diff）に絞って
# JSON で渡し、下の問いを添えるだけです。答えるのは esorp を走らせているエージェント自身。
# 器も形も正しく、専用句も使っていないのに事情を語っているコメントは、意味を読まなければ分かりません。
#
# 書かなければ層3 は開きません（ツールは既定を持たない）。書いても CI の赤/緑は変わりません——
# 判定は非決定的なので、そこに CI を賭けない。CI は層1・層2 の決定論だけで完結します。
#
# 問いは二値で答えられるものにしてください。カテゴリ（履歴 / 作業メモ / 判断）に分けさせると、
# 決定論で解けなかった分類問題を確率的な機械に押し付けることになります。
#
# review:
#   question: |
#     このコメントは、目の前のコードの説明ですか。それとも、事情・履歴・作業メモですか。
#     後者なら削除してください（履歴はバージョン管理が保持しています）。

# 今ある違反のスナップショット。載せた違反は報告されないが、一覧として見える状態で残る
# （esorp baseline update で書く。減る方向にしか動かない）。
baseline: .esorp-baseline.json
`
