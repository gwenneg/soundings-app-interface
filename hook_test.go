package main

import (
	"encoding/json"
	"fmt"
	"os"
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
		{"own post allowed", "mcp__plugin_soundings-app-interface_helper__post", true},
		{"bare server name gets no opinion", "mcp__helper__resolve", false},
		{"lookalike plugin gets no opinion", "mcp__plugin_evil_helper__resolve", false},
		{"unknown helper tool gets no opinion", "mcp__plugin_soundings-app-interface_helper__future", false},
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

// TestHooksMatcherMatchesPreApprovedTools guards against the matcher in
// hooks/hooks.json and the preApprovedTools set drifting apart: the
// matcher decides which calls reach the hook (performance), the set
// decides which are approved (policy) - they must name the same tools.
func TestHooksMatcherMatchesPreApprovedTools(t *testing.T) {
	data, err := os.ReadFile("hooks/hooks.json")
	if err != nil {
		t.Fatalf("reading hooks/hooks.json: %v", err)
	}
	var cfg struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parsing hooks/hooks.json: %v", err)
	}
	if len(cfg.Hooks.PreToolUse) != 1 {
		t.Fatalf("expected exactly 1 PreToolUse matcher entry, got %d", len(cfg.Hooks.PreToolUse))
	}

	matcherTools := make(map[string]bool)
	for _, tool := range strings.Split(cfg.Hooks.PreToolUse[0].Matcher, "|") {
		matcherTools[tool] = true
	}

	for tool := range preApprovedTools {
		if !matcherTools[tool] {
			t.Errorf("pre-approved tool %q is missing from the hooks.json matcher: its calls would never reach the hook", tool)
		}
	}
	for tool := range matcherTools {
		if !preApprovedTools[tool] {
			t.Errorf("matcher routes %q to the hook but it is not pre-approved: dead matcher entry", tool)
		}
	}
}
