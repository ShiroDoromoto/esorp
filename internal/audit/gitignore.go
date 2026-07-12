package audit

import (
	"os/exec"
	"path"
	"strings"
)

// gitFiles は、git が自分のものとして見ているファイルを root からの相対パスで返す（追跡している
// ものと、ignore されていない未追跡のもの）。照合を自前で書かず git 自身に聞くのは、.gitignore の
// 規則が入れ子・否定・グローバル設定を含み、写し取ると必ずずれるため。root が git リポジトリでない
// ときや git が無いときは ok が false になり、呼び手は絞り込みをしない（尊重できないだけで、走査は続く）。
func gitFiles(root string) (map[string]bool, bool) {
	cmd := exec.Command("git", "-c", "core.quotepath=false",
		"ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}

	files := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files[p] = true
		}
	}
	return files, true
}

// gitDirs は、files のいずれかが下にあるディレクトリを集める。走査の枝刈りに使う（gitignore された
// node_modules を、中まで歩いてから1つずつ落とす、ということをしない）。
func gitDirs(files map[string]bool) map[string]bool {
	dirs := map[string]bool{}
	for p := range files {
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	return dirs
}
