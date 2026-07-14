# 手元のゲート。コミットする前に `make check` を叩けば、CI が見るものと同じものが手元で見える。
# ゲートの中身はこの1ファイルにだけ置き、.github/workflows/ci.yml はここを呼ぶ（同じ検査を
# 2か所に書けば、いつか食い違う）。
#
# actionlint は go.mod の tool 依存として呼ぶ（go tool）。入れる手順が増えず、手元と CI で
# 同じ版が走る。版はここではなく go.mod にあるので、dependabot が上げどきを知らせてくれる。
# run: の中身は、shellcheck が PATH にあればそこまで見る。

.PHONY: check fmt vet test dogfood actions hooks

check: fmt vet test dogfood actions

# コミットメッセージにも層2（語彙）を当てる。コメントから追い出した履歴は、放っておくとここへ移る。
# 手元の git に一度だけ教える（core.hooksPath はリポジトリに焼けないので、各自が叩く）。
hooks:
	git config core.hooksPath .githooks
	@echo "commit-msg フックを有効にしました（esorp check --text -）"

fmt:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "gofmt が要るファイル:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet:
	go vet ./...

test:
	go test ./...

# esorp を esorp 自身のソースツリーに当てる。baseline を効かせた状態で緑を保つ。
dogfood:
	go run ./cmd/esorp check

# ワークフローと action.yml の壊れは、目で見ても出てこない（action の metadata は走らせても
# 黙って通る）。
actions:
	go tool actionlint -color
