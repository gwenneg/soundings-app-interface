package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// preApprovedTools are this plugin's own MCP tools the hook pre-approves,
// by their plugin-scoped names (mcp__plugin_<plugin>_<server>__<tool>).
// This set is the permission policy: a tool listed here never raises an
// interactive prompt. post is included by design - posting to the MR is
// still gated by the skill's explicit in-session question (it never posts
// without the user's yes), and user-configured deny/ask rules can restore
// a harness prompt for any of these tools at any time.
var preApprovedTools = map[string]bool{
	"mcp__plugin_soundings-app-interface_helper__resolve":  true,
	"mcp__plugin_soundings-app-interface_helper__annotate": true,
	"mcp__plugin_soundings-app-interface_helper__post":     true,
}

// runHook implements the plugin's PreToolUse hook: pre-approve this
// plugin's own helper MCP tools so a review run needs no prompts. The
// skill's allowed-tools frontmatter constrains what the turn may use but
// is not a permission grant, and a plugin cannot ship permission rules -
// an explicit hook "allow" is the plugin-side mechanism that skips the
// prompt.
//
// The hooks.json matcher already routes only these tools here, but matcher
// anchoring is undocumented regex behavior and the matcher can drift when
// edited - the exact-name check against preApprovedTools is the actual
// security boundary.
//
// Everything else gets no opinion, leaving the session's normal permission
// flow untouched.
func runHook(stdin io.Reader, stdout io.Writer) error {
	var in struct {
		ToolName string `json:"tool_name"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return fmt.Errorf("parsing hook input: %w", err)
	}
	if !preApprovedTools[in.ToolName] {
		return nil
	}
	return json.NewEncoder(stdout).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "soundings-app-interface: the plugin's own helper tool is pre-approved",
		},
	})
}
