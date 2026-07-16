package audit

import (
	"regexp"
	"testing"

	"github.com/ShiroDoromoto/esorp/internal/config"
	"github.com/ShiroDoromoto/esorp/internal/place"
	"github.com/ShiroDoromoto/esorp/internal/scan"
)

// textConfig は、層2 のルールを1つ持つ設定。where は呼び手が差し替える。
func textConfig(t *testing.T, where config.Where) *config.Config {
	t.Helper()

	cfg, err := config.TemplateConfig()
	if err != nil {
		t.Fatalf("テンプレートが読めない: %v", err)
	}
	cfg.Rules = []config.Rule{{
		ID:      "no-history",
		Pattern: `no longer|かつて`,
		Message: "変化を語っています。",
		Where:   where,
	}}
	cfg.Rules[0].Regexp = regexp.MustCompile(cfg.Rules[0].Pattern)
	return cfg
}

func TestText(t *testing.T) {
	cfg := textConfig(t, config.Where{})

	got := Text(cfg, "認証を直す\n\nこの関数はかつて同期だった。")
	if len(got) != 1 {
		t.Fatalf("違反 = %d 件, want 1\n%#v", len(got), got)
	}
	v := got[0]
	if v.ID != "no-history" || v.Message != "変化を語っています。" {
		t.Errorf("違反 = %q / %q", v.ID, v.Message)
	}
	if v.Line != 3 {
		t.Errorf("違反の行 = %d, want 3（当たった段落の先頭）", v.Line)
	}
	if v.Place != place.None || v.Kind != scan.KindNone {
		t.Errorf("器も種別も持たない入力なのに名乗っている: place=%s kind=%s", v.Place, v.Kind)
	}
	if v.Site.Path != "rules[0]" {
		t.Errorf("site = %q, want rules[0]", v.Site.Path)
	}
}

// TestTextLines は、違反が入力の中の行を持つことを見る。段落ごとに当てるので、どの段落を直せば
// よいのかが書き手に返る。
func TestTextLines(t *testing.T) {
	cfg := textConfig(t, config.Where{})

	got := Text(cfg, "認証を直す\n\nこの関数はかつて同期だった。\n\n待ち方も変えた。\n\nno longer 使わない。\n")
	if len(got) != 2 {
		t.Fatalf("違反 = %d 件, want 2\n%#v", len(got), got)
	}
	if got[0].Line != 3 || got[1].Line != 7 {
		t.Errorf("行 = %d, %d, want 3, 7", got[0].Line, got[1].Line)
	}
	if got[0].Text != "この関数はかつて同期だった。" {
		t.Errorf("当たった段落 = %q", got[0].Text)
	}
}

// TestTextClean は、ルールに触れない本文が通ることを見る。
func TestTextClean(t *testing.T) {
	if got := Text(textConfig(t, config.Where{}), "認証のトークンを検証する"); len(got) != 0 {
		t.Errorf("違反 = %d 件, want 0\n%#v", len(got), got)
	}
}

// TestTextFoldsWrappedBody は、折り返しで途切れた句に当たることを見る。コミットメッセージは
// 72 桁で折り返す流儀があり、畳まないと二語の句が改行をまたいで当たらない。
func TestTextFoldsWrappedBody(t *testing.T) {
	cfg := textConfig(t, config.Where{})

	if got := Text(cfg, "認証を直す\n\nこの関数は no\nlonger 同期ではない。"); len(got) != 1 {
		t.Errorf("折り返した句に当たらない: %d 件\n%#v", len(got), got)
	}
	if got := Text(cfg, "認証を直す\n\nこの関数はかつ\nて同期だった。"); len(got) != 1 {
		t.Errorf("全角の折り返しに当たらない: %d 件\n%#v", len(got), got)
	}
}

// TestTextSeverity は、素の本文に当てた違反にも強度が載ることを確かめる。語彙は esorp.yaml 1つに
// 保たれるので、強度の表も面をまたいで同じものが効く。Enforced が数えるのは enforce だけ。
func TestTextSeverity(t *testing.T) {
	cfg := textConfig(t, config.Where{})
	body := "この関数はかつて同期だった。"

	if got := Text(cfg, body); len(got) != 1 || got[0].Severity != config.SeverityEnforce || Enforced(got) != 1 {
		t.Errorf("severity: を書かない設定の違反 = %#v, want enforce 1件（書かれていない id は enforce）", got)
	}

	cfg.Severity = map[string]string{"no-history": config.SeverityAdvisory}
	got := Text(cfg, body)
	if len(got) != 1 || got[0].Severity != config.SeverityAdvisory {
		t.Fatalf("違反 = %#v, want advisory 1件", got)
	}
	if n := Enforced(got); n != 0 {
		t.Errorf("enforce の違反 = %d 件, want 0（advisory は報告に出るが数えない）", n)
	}
}

// TestTextWhere は、面の絞りを見る。取り出しの要らない入力を絞れる軸は syntax だけで、kind も path
// もその入力には無い。
func TestTextWhere(t *testing.T) {
	tests := []struct {
		name  string
		where config.Where
		want  int
	}{
		{"where を省いたルールは当たる", config.Where{}, 1},
		{"syntax: [text] は当たる", config.Where{Syntax: []string{config.SyntaxText}}, 1},
		{"ファミリで絞ったルールは当たらない", config.Where{Syntax: []string{"cstyle", "hash"}}, 0},
		{"kind で絞ったルールは当たらない", config.Where{Kind: []string{"line"}}, 0},
		{"path で絞ったルールは当たらない", config.Where{Path: []string{"**"}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Text(textConfig(t, tt.where), "この関数はかつて同期だった。")
			if len(got) != tt.want {
				t.Errorf("違反 = %d 件, want %d\n%#v", len(got), tt.want, got)
			}
		})
	}
}
