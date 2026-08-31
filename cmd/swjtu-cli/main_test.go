package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseAuthFetchArgs(t *testing.T) {
	target, output, settle, err := parseAuthFetchArgs([]string{
		"https://one.swjtu.edu.cn/",
		"--out", "page.html",
		"--wait", "2.5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if target != "https://one.swjtu.edu.cn/" || output != "page.html" {
		t.Fatalf("unexpected parsed values: target=%q output=%q", target, output)
	}
	if settle != 2500*time.Millisecond {
		t.Fatalf("unexpected settle duration: %s", settle)
	}
}

func TestParseAuthFetchArgsRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"javascript:alert(1)"},
		{"https://one.swjtu.edu.cn/", "--out"},
		{"https://one.swjtu.edu.cn/", "--wait", "-1"},
		{"https://one.swjtu.edu.cn/", "--unknown"},
	} {
		_, _, _, err := parseAuthFetchArgs(args)
		if err == nil {
			t.Errorf("expected error for args %q", strings.Join(args, " "))
		}
	}
}
