package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestAgentText は、エージェントが層3 に辿り着くのに要る3つ——走らせる口、層3 が誰の仕事か、
// esorp が LLM を呼ばないこと——が散文の出力にあることを確かめる。
func TestAgentText(t *testing.T) {
	var out strings.Builder
	if got := run([]string{"agent"}, &out, io.Discard); got != exitOK {
		t.Fatalf("agent = %d, want %d", got, exitOK)
	}

	for _, want := range []string{
		"esorp check --diff --format json",
		"Layer 3",
		"esorp calls no LLM",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("agent の出力に %q がありません:\n%s", want, out.String())
		}
	}
}

func TestAgentJSON(t *testing.T) {
	var out strings.Builder
	if got := run([]string{"agent", "--format", "json"}, &out, io.Discard); got != exitOK {
		t.Fatalf("agent --format json = %d, want %d", got, exitOK)
	}

	var doc struct {
		Version int    `json:"version"`
		Tool    string `json:"tool"`
		Layers  []struct {
			Layer         int  `json:"layer"`
			Deterministic bool `json:"deterministic"`
		} `json:"layers"`
		Cycle    []string `json:"cycle"`
		Commands []struct {
			Command string `json:"command"`
		} `json:"commands"`
		Output struct {
			Review string `json:"review"`
		} `json:"output"`
		ExitCodes []struct {
			Code int `json:"code"`
		} `json:"exit_codes"`
		Rules []string `json:"rules"`
	}
	if err := json.Unmarshal([]byte(out.String()), &doc); err != nil {
		t.Fatalf("JSON が壊れています: %v\n%s", err, out.String())
	}

	if doc.Version != 1 || doc.Tool != "esorp" {
		t.Errorf("version=%d tool=%q, want 1 / esorp", doc.Version, doc.Tool)
	}
	if len(doc.Layers) != 3 {
		t.Fatalf("層が %d 個、want 3", len(doc.Layers))
	}
	if l := doc.Layers[2]; l.Layer != 3 || l.Deterministic {
		t.Errorf("層3 = %+v、want layer=3 deterministic=false（意味の判定は決定論で解けない）", l)
	}
	if len(doc.Cycle) == 0 || len(doc.Rules) == 0 || doc.Output.Review == "" {
		t.Error("cycle / rules / output.review のどれかが空です（エージェントはここを読んで動く）")
	}

	var found bool
	for _, c := range doc.Commands {
		if c.Command == "esorp check --diff --format json" {
			found = true
		}
	}
	if !found {
		t.Error("commands に「esorp check --diff --format json」がありません（層3 が開く唯一の口）")
	}

	if len(doc.ExitCodes) != 3 {
		t.Errorf("exit_codes が %d 件、want 3（0 / 1 / 2）", len(doc.ExitCodes))
	}
}

func TestAgentBadArgs(t *testing.T) {
	cases := [][]string{
		{"agent", "--format", "yaml"},
		{"agent", "check"},
	}
	for _, args := range cases {
		var out strings.Builder
		if got := run(args, &out, io.Discard); got != exitConfig {
			t.Errorf("%v = %d, want %d", args, got, exitConfig)
		}
		if out.Len() > 0 {
			t.Errorf("%v: 撥ねたのに標準出力へ書いています:\n%s", args, out.String())
		}
	}
}
