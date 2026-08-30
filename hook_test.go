package main

import (
	"fmt"
	"strings"
	"testing"
)

const testRoot = "/Users/x/.claude/plugins/cache/m/soundings-app-interface/1.0.0"

func TestIsOwnHelperCommand(t *testing.T) {
	cases := []struct {
		name, command string
		want          bool
	}{
		{"resolve with IID", "go -C " + testRoot + " run . resolve 12345", true},
		{"resolve with URL", "go -C " + testRoot + " run . resolve https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/12345", true},
		{"annotate one arg", "go -C " + testRoot + " run . annotate /tmp/soundings-report.md", true},
		{"annotate with feedback URL", "go -C " + testRoot + " run . annotate /tmp/soundings-report.md https://feedback.example.com/form", true},
		{"post is not pre-approved", "go -C " + testRoot + " run . post 12345 /tmp/report.md", false},
		{"hook itself is not pre-approved", "go -C " + testRoot + " run . hook " + testRoot, false},
		{"wrong plugin root", "go -C /somewhere/else run . resolve 12345", false},
		{"command chaining rejected", "go -C " + testRoot + " run . resolve 12345; rm -rf /", false},
		{"pipe rejected", "go -C " + testRoot + " run . resolve 12345 | tee /tmp/x", false},
		{"substitution rejected", "go -C " + testRoot + " run . annotate $(echo /etc/passwd)", false},
		{"redirection rejected", "go -C " + testRoot + " run . resolve 12345 > /tmp/out", false},
		{"quoted arg rejected (conservative)", "go -C " + testRoot + " run . annotate '/tmp/my report.md'", false},
		{"resolve without arg rejected", "go -C " + testRoot + " run . resolve", false},
		{"resolve with extra arg rejected", "go -C " + testRoot + " run . resolve 1 2", false},
		{"annotate with three args rejected", "go -C " + testRoot + " run . annotate a b c", false},
		{"different binary rejected", "python -C " + testRoot + " run . resolve 12345", false},
		{"empty command rejected", "", false},
	}
	for _, c := range cases {
		if got := isOwnHelperCommand(c.command, testRoot); got != c.want {
			t.Errorf("%s: isOwnHelperCommand(%q) = %v, want %v", c.name, c.command, got, c.want)
		}
	}
}

func TestIsOwnHelperCommandEmptyRoot(t *testing.T) {
	if isOwnHelperCommand("go -C  run . resolve 12345", "") {
		t.Error("an empty plugin root must never match")
	}
}

func TestRunHook(t *testing.T) {
	cases := []struct {
		name, tool, command string
		wantAllow           bool
	}{
		{"own resolve allowed", "Bash", "go -C " + testRoot + " run . resolve 12345", true},
		{"own annotate allowed", "Bash", "go -C " + testRoot + " run . annotate /tmp/r.md", true},
		{"post gets no opinion", "Bash", "go -C " + testRoot + " run . post 12345 /tmp/r.md", false},
		{"other bash gets no opinion", "Bash", "ls -la", false},
		{"other tools get no opinion", "Read", "go -C " + testRoot + " run . resolve 12345", false},
	}
	for _, c := range cases {
		in := fmt.Sprintf(`{"tool_name":%q,"tool_input":{"command":%q}}`, c.tool, c.command)
		var out strings.Builder
		if err := runHook(strings.NewReader(in), &out, testRoot); err != nil {
			t.Errorf("%s: runHook: %v", c.name, err)
			continue
		}
		gotAllow := strings.Contains(out.String(), `"permissionDecision":"allow"`)
		if gotAllow != c.wantAllow {
			t.Errorf("%s: allow=%v, want %v (output %q)", c.name, gotAllow, c.wantAllow, out.String())
		}
		if !c.wantAllow && out.String() != "" {
			t.Errorf("%s: expected no output, got %q", c.name, out.String())
		}
	}
}

func TestRunHookErrorsOnMalformedInput(t *testing.T) {
	var out strings.Builder
	if err := runHook(strings.NewReader("not json"), &out, testRoot); err == nil {
		t.Fatal("expected an error on malformed input")
	}
}
