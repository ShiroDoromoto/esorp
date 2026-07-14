package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// textConfig は、層2 のルールを持つ設定。no-history は面を絞らないので、ファイルにも取り出しの
// 要らない入力にも当たる。files-only は where.syntax で絞ってあるので、text には当たらない。
const textConfig = `
syntax:
  cstyle:
    files: ["**/*.go"]
    mode: content-only
rules:
  - id: no-history
    pattern: "かつて|no longer"
    message: "変化を語っています。今の姿だけを書いてください。"
  - id: files-only
    pattern: "禁句"
    message: "コメントには書けません。"
    where:
      syntax: [cstyle]
`

// checkText は、本文を標準入力に流して check --text - を走らせる。
func checkText(t *testing.T, cfgPath, body string, args ...string) (int, string) {
	t.Helper()

	var stdout strings.Builder
	full := append([]string{"check", "--config", cfgPath, "--text", "-"}, args...)
	code := runInput(full, strings.NewReader(body), &stdout, io.Discard)
	return code, stdout.String()
}

func TestCheckTextExitCodes(t *testing.T) {
	cfgPath := tree(t, textConfig, "")

	tests := []struct {
		name string
		body string
		want int
	}{
		{"語彙に触れない本文は適合", "認証のトークンを検証する", exitOK},
		{"語彙に触れる本文は違反あり", "認証を直す\n\nこの関数はかつて同期だった。", exitViolated},
		{"折り返しで途切れた句にも当たる", "認証を直す\n\nこの関数は no\nlonger 同期ではない。", exitViolated},
		{"面で絞ったルールは当たらない", "禁句を書く", exitOK},
		{"空の入力は適合", "", exitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := checkText(t, cfgPath, tt.body); got != tt.want {
				t.Errorf("check --text - = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCheckTextReportsViolation(t *testing.T) {
	code, out := checkText(t, tree(t, textConfig, ""), "認証を直す\n\nこの関数はかつて同期だった。")
	if code != exitViolated {
		t.Fatalf("code = %d, want %d", code, exitViolated)
	}
	for _, want := range []string{"no-history", "かつて同期だった", "変化を語っています"} {
		if !strings.Contains(out, want) {
			t.Errorf("報告に %q が現れない:\n%s", want, out)
		}
	}
}

func TestCheckTextJSON(t *testing.T) {
	code, out := checkText(t, tree(t, textConfig, ""), "この関数はかつて同期だった。", "--format", "json")
	if code != exitViolated {
		t.Fatalf("code = %d, want %d", code, exitViolated)
	}

	var got struct {
		Version int    `json:"version"`
		Surface string `json:"surface"`
		Layers  struct {
			Applied    []string `json:"applied"`
			NotApplied []string `json:"not_applied"`
		} `json:"layers"`
		Baseline bool `json:"baseline"`
		Summary  struct {
			Violations int `json:"violations"`
		} `json:"summary"`
		Violations []struct {
			Line    int    `json:"line"`
			ID      string `json:"id"`
			Text    string `json:"text"`
			Message string `json:"message"`
		} `json:"violations"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, out)
	}
	if got.Summary.Violations != 1 || len(got.Violations) != 1 {
		t.Fatalf("違反 = %d 件, want 1\n%s", len(got.Violations), out)
	}
	if got.Violations[0].ID != "no-history" || got.Violations[0].Line != 1 {
		t.Errorf("違反 = %q（行 %d）, want no-history（行 1）", got.Violations[0].ID, got.Violations[0].Line)
	}
	if strings.Contains(out, `"path"`) {
		t.Errorf("パスを持たない入力なのに、欄がある:\n%s", out)
	}
	if got.Surface != "text" || got.Baseline {
		t.Errorf("surface = %q / baseline = %v, want text / false", got.Surface, got.Baseline)
	}
	if len(got.Layers.NotApplied) != 2 {
		t.Errorf("当たらない層を告げていない: %v", got.Layers)
	}
}

// TestCheckTextSaysWhatDoesNotApply は、適合したときも、当たらない層を告げることを見る。黙って通すと、
// 通ったことが「層1 も通った」「baseline で抑えられる」と読まれる。
func TestCheckTextSaysWhatDoesNotApply(t *testing.T) {
	code, out := checkText(t, tree(t, textConfig, ""), "認証のトークンを検証する")
	if code != exitOK {
		t.Fatalf("code = %d, want %d", code, exitOK)
	}
	for _, want := range []string{"層1（器・書式）は当たりません", "baseline はありません"} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が現れない:\n%s", want, out)
		}
	}
}

// TestCheckTextJSONEmpty は、違反が無くても violations が空配列であることを見る（null にすると、
// 読み手が場合分けを強いられる）。
func TestCheckTextJSONEmpty(t *testing.T) {
	code, out := checkText(t, tree(t, textConfig, ""), "認証のトークンを検証する", "--format", "json")
	if code != exitOK {
		t.Fatalf("code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(out, `"violations": []`) {
		t.Errorf("violations が空配列でない:\n%s", out)
	}
}

// TestCheckTextRejects は、使い方の誤りを黙って受けないことを見る。
func TestCheckTextRejects(t *testing.T) {
	cfgPath := tree(t, textConfig, "")

	tests := []struct {
		name string
		args []string
	}{
		{"--text はファイルを受けない", []string{"check", "--config", cfgPath, "--text", "msg.txt"}},
		{"--diff とは併せられない", []string{"check", "--config", cfgPath, "--text", "-", "--diff"}},
		{"余分な引数は受けない", []string{"check", "--config", cfgPath, "--text", "-", "HEAD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr strings.Builder
			if got := runInput(tt.args, strings.NewReader(""), io.Discard, &stderr); got != exitConfig {
				t.Errorf("code = %d, want %d", got, exitConfig)
			}
			if stderr.Len() == 0 {
				t.Error("理由を言っていない")
			}
		})
	}
}
