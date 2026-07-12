package main

import (
	"io"
	"strings"
	"testing"
)

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"引数なしは使い方を出して設定エラー", nil, exitConfig},
		{"check は適合", []string{"check"}, exitOK},
		{"check の未知のフラグは設定エラー", []string{"check", "--nope"}, exitConfig},
		{"help は適合", []string{"help"}, exitOK},
		{"未知のサブコマンドは設定エラー", []string{"nope"}, exitConfig},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := run(tt.args, io.Discard, io.Discard)
			if got != tt.want {
				t.Errorf("run(%q) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunHelpPrintsUsageToStdout(t *testing.T) {
	var stdout strings.Builder
	if got := run([]string{"help"}, &stdout, io.Discard); got != exitOK {
		t.Fatalf("run(help) = %d, want %d", got, exitOK)
	}
	if !strings.Contains(stdout.String(), "使い方:") {
		t.Errorf("help が使い方を出していない: %q", stdout.String())
	}
}
