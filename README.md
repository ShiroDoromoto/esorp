# esorp

コメントの置き場所と書式を、`esorp.yaml` の宣言に照らして監査する。

落とそうとしているのは、目の前のコードの説明ではないコメントです。履歴（「以前はこうだった」）、
事情（「議論の結果こうした」）、作業メモ（課題番号、次にやること）。これらは書かれた瞬間から
陳腐化を始めます。コードは変わってもコメントは追随せず、残ったコメントが将来の読み手
（次の AI エージェントを含む）をミスリードします。

散文の規約（CONTRIBUTING に「履歴を書くな」と書く）では止まりません。エージェントは規約を読んで
なお書きます。機械的に落とす以外に手がない、というのが esorp の前提です。

## 三層

| 層 | 見るもの | 決定論的か | 誰が答えるか |
| --- | --- | --- | --- |
| **層1: 器と書式** | コメントが**どこに**入っているか、どんな**形**をしているか | する | esorp（CI と pre-commit がそのまま赤/緑にする） |
| 層2: 語彙 | コメント本文に現れる、プロジェクト固有の専用句 | する | esorp。ただし語彙を持つのは設定ファイルだけ（`init` がプリセットを書き込む。消すのも足すのもあなた） |
| 層3: 意味 | 層1・層2 を通り抜けたコメントが、コードの説明か、事情・履歴・作業メモか | しない | esorp を走らせている**エージェント自身**（[層3](#層3--エージェントが答える) 参照） |

層1 は言語仕様から導けるので、プロジェクトに依存しません。層2・層3 は機械判定できないプロジェクト
固有の都合であり、ツールが既定を持つ資格がありません。

**esorp は「書けるものを減らす」方向しか持ちません。** `@param` を書け・doc を書け、と足す方向は
既存の doc lint（checkstyle、eslint 系、`missing_docs`）の領分です。

## 入れる

```sh
# インストールスクリプト（macOS / Linux）
curl -fsSL https://github.com/ShiroDoromoto/esorp/releases/latest/download/install.sh | sh

# Homebrew
brew install ShiroDoromoto/esorp/esorp

# Scoop（Windows）
scoop bucket add esorp https://github.com/ShiroDoromoto/scoop-esorp
scoop install esorp

# Go
go install github.com/ShiroDoromoto/esorp/cmd/esorp@latest
```

Debian / RPM 系のパッケージは [Gemfury](https://fury.io) から出ています
（`https://apt.fury.io/shirodoromoto/` / `https://yum.fury.io/shirodoromoto/`）。

## 来歴を確かめる

リリースの資産（アーカイブ、`install.sh`、deb / rpm）には、GitHub の
[artifact attestation](https://docs.github.com/actions/security-guides/using-artifact-attestations-to-establish-provenance-for-builds)
が付いています。「このリポジトリの、このコミットから、この CI で焼かれた」ことを、受け取った側で確かめられます。

```sh
gh attestation verify esorp_<version>_darwin_arm64.tar.gz --repo ShiroDoromoto/esorp
```

確かめられる範囲は、入れ方で変わります。隠さずに書きます。

| 入れ方 | 来歴 |
| --- | --- |
| install.sh / アーカイブ / deb / rpm | 落としたファイルに `gh attestation verify` が当たる |
| Homebrew / Scoop | 資産そのものはリリースのものと同じバイト列だが、`brew` / `scoop` は来歴を見ない |
| `go install` | 手元でソースからビルドするので、来歴の対象外 |

## 使う

```sh
esorp init                    # 設定ファイル（esorp.yaml）を生成する
esorp check                   # ツリー全体を監査する
esorp check --diff [<ref>]    # 変更分だけを監査する（既定の <ref> は origin/HEAD）
esorp check --text <src>      # 渡された本文に層2（語彙）だけを当てる（- は標準入力、他はパス）
esorp explain <file>:<line>   # その行が、なぜ違反で、どう始末するのかを説明する
esorp baseline update         # 今ある違反をスナップショットする（減る方向のみ）
esorp lexicon --try <re>      # 層2 に足す前に、候補の語彙を自分のコーパスで測る
esorp review [<path>...]      # 層1・層2 を通り抜けたコメントを、問いを添えて渡す（層3）
esorp agent                   # エージェント向けの入口
```

導入初日は、既存コードの違反が大量に出ます（`init` が書き込む層2 のプリセットは、過去に書かれた
コメントにも当たります）。全部直すのは現実的ではないので、いったん凍結します。

```sh
esorp baseline update --allow-new   # 今ある違反を baseline に載せる（以後は報告されない）
```

baseline はラチェットです。**減る方向にしか動きません** — 直した違反は落ち、新しい違反は載りません。
抑えている件数は毎回の出力に出るので、隠れることはありません。

**インライン抑制コメント（`// esorp:ignore`）はありません。** 抑制コメント自体が「許可されていない
器のコメント」になって自己矛盾しますし、違反を消す代わりに抑制を足す抜け道になります。例外は
baseline に載せます（＝一覧として見える状態にする）。

終了コードは `0` 適合 / `1` 違反あり / `2` 設定エラーの3値です。`1` を出すのは `severity: enforce`
（既定）の違反だけで、`advisory` にした違反は報告に出ますが CI を落としません。

## pre-commit

[pre-commit](https://pre-commit.com) から呼べます。`.pre-commit-config.yaml` に書きます。

```yaml
repos:
  - repo: https://github.com/ShiroDoromoto/esorp
    rev: ""               # pre-commit autoupdate が最新のタグで埋めます
    hooks:
      - id: esorp
      - id: esorp-commit-msg
        stages: [commit-msg]
```

見るのは HEAD から作業ツリーまでに増えた行に重なるコメントだけです。既にあった違反は素通しします
（それは baseline の仕事）。コミットのたびにツリー全体を見たいなら `args: [check]` で上書きします。

`esorp-commit-msg` は、コミットメッセージの本文に層2（語彙）を当てます（次節）。commit-msg は
pre-commit の既定のステージではないので、`stages: [commit-msg]` を添えます。フックそのものを
`.git/hooks` に入れるには、一度だけ `pre-commit install --hook-type commit-msg` を走らせます。

## コミットメッセージにも同じ語彙を当てる

コメントから履歴・事情を追い出すと、同じものがコミットメッセージへ移ります。そこは公開リポジトリの
恒久記録で、あとから直せません。フックに別の正規表現を書けば、禁止語彙が `esorp.yaml` とフックの
二箇所に分裂し、必ずドリフトします — **禁止語彙の源泉は一つ**でなければなりません。

`check --text` は、渡された文字列そのものを本文として読みます（`-` は標準入力、それ以外はファイルの
パスで、中身がまるごと本文になります）。

```sh
printf 'この関数はかつて同期だった。\n' | esorp check --text -
```

```
1  no-history
  この関数はかつて同期だった。
  変化を語っています。今のコードが何であるかだけを書いてください。
  「以前はこうだった」はバージョン管理が保持しています。

1 violations
Only layer 2 (lexicon) applied. Layer 1 (vessel and form) does not apply (the body passed in has no vessel).
There is no baseline (a one-off input has no key for a suppression to stand on).
```

当たるのは層2（語彙）だけです。素のテキストは器を持たないので、**層1（器・書式）は当たりません**。
baseline も効きません。終了コード（`0` / `1` / `2`）と `--format`（text | json）は、ツリーの監査と
同じです。

**esorp は git を知りません。** 本文を渡すのはフックの仕事です。`.git/hooks/commit-msg`:

```sh
#!/bin/sh
esorp check --text "$1"
```

pre-commit から呼ぶなら、この節を自分で書く必要はありません — `esorp-commit-msg` が同じことをします
（pre-commit はシェルを介さずフックを実行し、メッセージファイルのパスを引数で足すので、パスで受ける
口が要ります）。

同じ口が PR 本文にもリリースノートにも挿さります（`gh pr view --json body -q .body | esorp check --text -`）。
`check-commit-msg` のような専用の口を作らないのは、作った瞬間に特定のワークフローがツールへ焼き込まれる
からです。esorp が読むのは渡された本文であって、`.git` の場所を探しには行きません。

`esorp init` が書き込む `no-history` は `where` を省いているので、**この口にもそのまま当たります**。
面を絞ったルール（`where: syntax: [cstyle]`）を、この口にも当てたいなら、面に `text` を足します（次節）。

## GitHub Action

```yaml
- uses: actions/checkout@v7
  with:
    fetch-depth: 0        # --diff は分岐点を取る。浅いクローンでは取れない
- uses: ShiroDoromoto/esorp@v0
```

`v0` はメジャータグで、リリースのたびに最新のリリースへ移ります。特定の版に留めたいなら、
その版のタグを直に書きます。

引数を渡さなければ、pull request では変更分だけを（`check --diff origin/<base>`）、それ以外では
ツリー全体を（`check`）見ます。`with: {args: "check"}` で上書きできます。

## esorp.yaml の要点

`esorp init` が生成した設定は、その時点であなたのものです。**esorp が勝手に書き換えることはありません。**
ツールを更新しても既定は変わりません。既定ルールの改善は `esorp init --diff` から見て、取り込むか
どうかをあなたが決めます（`--format json` で機械可読にも出ます）。

### 層1 — 器と書式

```yaml
syntax:
  cstyle:
    files: ["**/*.go", "**/*.rs", "**/*.ts"]
    mode: structural

    # 許可する「器」。ここに列挙されていない器のコメントは、中身が何であれ違反
    allow:
      - place: header               # ファイル冒頭（ライセンス等）
      - place: doc                  # 宣言に紐づく説明
        form:                       # 器の中の「形」。語彙は見ない
          subject: required         # 1行目が、紐づく宣言の名前で始まること
          headings: deny            # 見出しを書けない（履歴は見出しを付けて書かれる）
          paragraphs: 1             # 段落は1つ（背景を段落として付け足せない）
      - place: trailing             # 行末。ラベル必須
        label: ["SAFETY:", "TODO:"]
    # place: leading / orphan は列挙していない ＝ 許可しない
```

器は `header` / `doc` / `leading` / `trailing` / `orphan` の5つ。`mode: structural` は器を見る
モードで、`content-only` は器の概念が無いファイル（YAML、シェル、Markdown）に使います
（コメントが1種類しかないので、器の列挙が選択の軸として働かない）。

拡張子も既知の名前も持たないファイル（生成物、フック）は、字句を名指しできます。

```yaml
syntax:
  gen:
    family: cstyle
    lang: go            # 字句を名指しする（省略時は拡張子・名前から引く）
    files: ["tools/gen"]
```

書ける名前は go / rust / typescript / tsx / javascript / jsx / css / sgml / shell / yaml / toml /
make / dockerfile / gitignore / powershell。ファミリと食い違う `lang:` は設定エラーです。

プリセットの字句で読めない拡張子（新しい言語のインストーラや小道具）は、コメント記法を宣言できます。
`mode: content-only` 限定で、行コメントとブロックコメントの記号だけを書きます。宣言があれば、
未登録の拡張子でも読みます（ツールの更新を待たずに網を張れる）。

```yaml
syntax:
  nsis:
    files: ["**/*.nsh"]
    mode: content-only
    comments:
      line: [";", "#"]        # 行コメントの記号（複数書ける）
      block: [["/*", "*/"]]   # ブロックコメントの開き・閉じの対（複数書ける）
```

`comments:` は読み方を丸ごと決めるので、`lang:` や `family:` とは併記できません（食い違うため設定
エラー）。器の宣言の解析が要る `mode: structural` にも書けません。

### 層2 — 語彙

**ツールのコードは語彙を持ちません。** 語彙があるのは、あなたの `esorp.yaml` の中だけです。
`esorp init` は、そこにプリセットを2つ書き込んで吐きます。

```yaml
rules:
  - id: no-history
    pattern: '(?i)(we|it|this|that|they|these|those|which|there)\s+used\s+to|かつて|従来は|(^|[\s。、（(「『:：・])以前は'
    message: |
      変化を語っています。今のコードが何であるかだけを書いてください。
      「以前はこうだった」はバージョン管理が保持しています。

  - id: internal-ref
    pattern: '#\d+'          # ← 一例。自分の採番規約に直してください
    message: |
      追跡番号への参照です。読み手（将来の参加者・外部の読者・次のエージェント）は追跡システムを
      辿れません。番号ではなく、コードだけで読める形で書いてください。
    where:
      syntax: [cstyle-go, cstyle-rust, cstyle-ts, cstyle-js, hash, sgml, cssblock]
```

**書き込まれた時点で、これはあなたのものです。** 要らなければ消してください。足したければ足して
ください。プリセット由来か自作かを、`rules:` は覚えていません（出自を分けると、消していいのかが
分からなくなります）。ツールを更新しても手元の設定は変わらないので、こちらの改善を取り込むかどうかは
`esorp init --diff` を見て決めます。

**`internal-ref` は、そのままでは他人の採番規約を見ています。** 何が参照かはプロジェクトごとに
違う（`#123` / `PROJ-456` / `GH-12` / 独自接頭辞）ので、ツールは参照の形を知りません。吐かれた
パターンは出発点として流し込んだ一例で、**直す前提**です。ツールが黙って決めていたことが、設定の上に
見える形で出ている、という状態です。

`where.syntax` がコメントの面だけを並べていて `text` が無いのは、**コミットメッセージが参照の正しい
置き場**だからです。コードから追い出した参照の行き先がそこなので、追いかけて塞ぐと行き場が
無くなります。履歴を止める `no-history` が `text` にも当たるのと対になります（履歴には行き先が
ありません）。なお `where.syntax` は列挙しかできないので、**使わない言語の `syntax` エントリを
消すときは、この並びからも消してください**（`syntax:` に無い名前は設定エラーです）。

**なぜこの句だけなのか。** 文字列マッチは必ず誤検知します。4つのコーパス（Go 標準ライブラリ
248,920コメント、日本語のプロジェクト2件 8,209、英語のプロジェクト1件 1,335）で測って、偽陽性が
ほとんど出なかった句だけが残っています。落とした句・狭めた句と、その理由:

| 素で当てると | 実測 |
| --- | --- |
| `old` / `before` / `legacy` / `旧` / `previously` | 時間や新旧を指す汎用語は、ほぼ全部が偽陽性（「〜する前に」「古い方の値」は正当な説明）。**入れていません** |
| `deprecated` | 316件中245件が Go の `Deprecated:` マーカー＝API の現在の状態であって、履歴ではない。**入れていません** |
| `no longer` | 「今そうでない状態」の記述（"closed when no longer needed"）が支配的。安い正規表現では救えない。**入れていません** |
| `used to` | 93% が「〜するために使われる」という目的の用法。**主語（`this` / `we` / …）を前に置いた形**だけを見ます |
| `従来` | 4分の3 が「従来どおり／従来挙動」＝今の挙動の説明。**「従来は」**だけを見ます |
| `以前` | 半分以上が偽陽性（「v2 以前の形式」＝互換の境界）。係助詞「は」を伴い、**直前が文頭・句読点・空白**のときだけが履歴を指しました |

**誤検知するガードは例外指定を誘発し、やがてツールごと無視されます。** だから足す前に、自分の
コーパスで測ってください。

測る口があります。候補パターンをツリーの全コメントに当て、件数と当たった本文を見せます。

```sh
esorp lexicon --try '(?i)previously'
```

```
internal/store/index.go:3:1  place=doc kind=line
  // Append は、索引の末尾に足す。previously 書き出した領域は読み直さない。

(?i)previously matched 1 (71 files / 605 comments, 0.17%)

Breakdown by surface:
  cstyle              1 / 553 comments (0.18%)
  hash                0 / 52 comments (0.00%)

The text surface (check --text) cannot be measured. The body passed in lives outside the tree, and there is no corpus to match against.
Decide rules for this surface by reading the matches (this is not 0 matches — it is not measured).
esorp does not judge true positive from false. Read the matches and decide whether to add the term.
```

当たりを全部並べたあと、件数と、それが全コメントに占める割合を書きます。**真陽性か偽陽性かは
判定しません** — 当たりを読んで、足すかどうかを決めるのはあなたです。

内訳は**面（`syntax` エントリ）ごと**に出ます。Go の doc と YAML のコメントでは書かれる語彙が
そもそも違うので、ある面で誤検知ゼロだったパターンが、別の面では当たりまくります。全体の件数だけを
見ていると、その偏りが平均に埋もれます。

**`text` 面（`check --text`）は測れません。** 渡される本文はツリーの外にあり、当てるコーパスが
ありません（0 件と出しているのではなく、測っていないということです）。

当てる本文は層2 とまったく同じ（折り返しを畳んだもの）なので、**ここで出た件数が、`rules:` に
足したときに当たる件数そのもの**です。層1（器と書式）は当てません——語彙の精度は、コメントが
どこに置かれているかとは関係なく決まるからです。当たっても違反ではないので、終了コードは `0` です。

ルール自身を説明するコメントは、その句を例として引用するので当たります（引用は「使用」ではないのに
当たる）。**インライン抑制コメントは持たない**ので、手当ては3つです。

1. 引用を使わない言い方に直す（たいていこれで済み、そして読みやすくなります）
2. `where.path` でルールの当たる範囲から外す（ルール自身を実装するファイルなど）
3. baseline に載せる（一覧として見える状態で残ります）

句をテストデータや設定に書くぶんには当たりません。**文字列リテラルはコメントではない**からです。

`where` を省くと、全ての `syntax` エントリ・全ての kind・全てのファイル、そして `check --text` に
渡された本文にも当たります（共有が既定で、例外だけを宣言します）。絞るなら:

```yaml
    where:
      syntax: [cstyle-go, text]                  # 省略時は全エントリ。text は取り出しの要らない入力の面
      kind: [line, block]                        # 省略時は全 kind
      path: ["**/*.go", "!internal/legacy/**"]   # 省略時は全ファイル。「!」始まりは除外
```

`text` は `where.syntax` の**予約値**で、`check --text` に渡された本文の面を指します。`syntax:` の
エントリとしては書けません（ファイルを持たない入力なので、拾う対象がありません）。この入力を絞れる軸は
`syntax` だけです — `kind`（コメントの種別）も `path`（ファイル）も持たないので、**その軸で絞った
ルールは、この面には当たりません**。

### 層3 — エージェントが答える

```yaml
review:
  question: |
    このコメントは、目の前のコードの説明ですか。それとも、事情・履歴・作業メモですか。
    後者なら削除してください（履歴はバージョン管理が保持しています）。
```

**esorp は LLM を呼びません。** API キーも課金もネットワークも要りません。層3 は、層1・層2 を
通り抜けたコメントを、変更分に絞って機械可読で渡し、この問いを添えるだけの口です。答えるのは
**esorp を走らせているエージェント自身**です。

```sh
esorp check --diff --format json
```

設定に `review:` があり、かつ `--diff --format json` のときだけ、出力に `review` が乗ります。

```json
{
  "review": {
    "question": "このコメントは、目の前のコードの説明ですか。…",
    "comments": [
      {"path": "internal/store/index.go", "line": 3, "col": 1,
       "place": "doc", "kind": "line", "text": "// Append は、索引の末尾に足す。…"}
    ]
  }
}
```

**層3 は CI の赤/緑を変えません。** 判定は非決定的なので、そこに CI を賭けません。CI は層1・層2 の
決定論だけで完結します（オフライン・再現可能）。器も形も正しく、専用句も使っていないのに事情を
語っているコメントは、意味を読まなければ分かりません。その網は、コードを書いているエージェント
自身にしか張れません。

問いは**二値で答えられる**ものにしてください。カテゴリ（履歴 / 作業メモ / 判断）に分けさせると、
決定論で解けなかった分類問題を、確率的な機械に押し付けることになります。

`check --diff` が渡すのは「今書いたもの」だけです。**導入初日に、既にあるコメントを一度だけ読ませ
たい**なら `esorp review` を使います。

```sh
esorp review                  # ツリー全体の、通り抜けたコメントを全部渡す
esorp review internal/scan/   # パスで絞る（多すぎると読む側が破綻します）
esorp review --format text    # 人が眺めるとき
```

判定しないので、**終了コードは常に 0** です（層3 は CI に関与しません。だから `check` とコマンドが
分かれています）。

## 更新するとき

v0 なので、破壊的変更があります。手元の設定はツールの更新では変わらないので、**直すのはあなた**です。

### `form.refs` を廃しました（参照は `rules:` へ）

ツールが「参照とは `#123` のことだ」と知っているのをやめました。何が参照かはプロジェクトごとに違う
ので、汎用ツールが特定の採番規約を決め打ちするのは、esorp が層2 について引いた線——**ツールは自前の
語彙を持たない**——と食い違っていました。移行先は上の [`internal-ref`](#層2--語彙) です。

**設定エラー（exit 2）になるのは、次の3キーです。3つまとめて消してください。**

| キー | 出方 | 書いていたなら |
| --- | --- | --- |
| `allow[].form.refs` | `unknown field "refs"`（行番号つき） | `refs: deny` を書いていた |
| `disposition.form-refs` | `not a known violation id` | 始末のしかたを書いていた |
| `severity.form-refs` | `not a known violation id` | 強制の強度に載せていた |

まとめて消すのは、往復を省くためです。設定は読む → 検める の二段構えなので、`refs: deny` を消して
初めて `disposition.form-refs` のエラーが出ます。**`esorp init --diff` は、この3つを消したあとから
効きます**（設定が読めないうちは、そこで止まります）。消してから走らせると、`internal-ref` が
`in the template only` として挙がるので、そこで取り込めます。

**`form-refs` という id は空きました。** 層1 の予約 id ではなくなったので、`rules[].id` として
あなたが持てます（`severity` もその id で解決します）。移行先の id は `internal-ref` を薦めますが、
`form-refs` のまま `rules:` へ移す道も塞いでいません。

## エージェントに使わせる

`AGENTS.md` / `CLAUDE.md` に「このリポジトリでは esorp を使う。まず `esorp agent` を読め」と
書いてください。エージェントは、そこから層3 に辿り着けます。

```sh
esorp agent                 # 三層・サイクル・コマンド・出力の読み方・終了コードを散文で出す
esorp agent --format json   # 同じ地図を機械可読で出す
```

## やらないこと

| やらないこと | 理由 |
| --- | --- |
| コメントの自動生成・自動書き換え | 監査に徹する。書き換えは失敗時の被害が大きい |
| コメントを「行き先」へ自動移送すること | 行き先はツールの外側にある。そもそも違反コメントの大半に行き先など無く、正解は削除 |
| インライン抑制コメント | 自己矛盾するし、抜け道になる。例外は baseline に載せる |
| doc コメントに必須セクションを強制すること | 既存の言語固有 lint の領分 |
| 文体・語法のチェック | [vale](https://vale.sh) の領分 |

## ライセンス

Apache-2.0
