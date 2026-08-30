package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestRunHook(t *testing.T) {
	cases := []struct {
		name, tool string
		wantAllow  bool
	}{
		{"own resolve allowed", "mcp__plugin_soundings-app-interface_helper__resolve", true},
		{"own annotate allowed", "mcp__plugin_soundings-app-interface_helper__annotate", true},
		{"post gets no opinion", "mcp__plugin_soundings-app-interface_helper__post", false},
		{"bare server name gets no opinion", "mcp__helper__resolve", false},
		{"lookalike plugin gets no opinion", "mcp__plugin_evil_helper__resolve", false},
		{"Bash gets no opinion", "Bash", false},
		{"Read gets no opinion", "Read", false},
	}
	for _, c := range cases {
		in := fmt.Sprintf(`{"tool_name":%q,"tool_input":{}}`, c.tool)
		var out strings.Builder
		if err := runHook(strings.NewReader(in), &out); err != nil {
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
	if err := runHook(strings.NewReader("not json"), &out); err == nil {
		t.Fatal("expected an error on malformed input")
	}
}
