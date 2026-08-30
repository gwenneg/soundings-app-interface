package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// runHook implements the plugin's PreToolUse hook: pre-approve this
// plugin's own resolve and annotate MCP tools so a review run needs no
// prompts for them. The skill's allowed-tools frontmatter constrains what
// the turn may use but is not a permission grant, and a plugin cannot ship
// permission rules - an explicit hook "allow" is the plugin-side mechanism
// that skips the prompt. User-configured deny and ask rules still apply
// regardless of the allow.
//
// post is deliberately NOT pre-approved: it is the one outward-facing
// action (it posts the report to the MR under the user's identity), so
// its interactive prompt stays as a harness-level gate on top of the
// skill's ask-before-posting instruction.
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
	// The plugin-scoped names the harness gives this plugin's MCP tools:
	// mcp__plugin_<plugin>_<server>__<tool>.
	if in.ToolName != "mcp__plugin_soundings-app-interface_helper__resolve" &&
		in.ToolName != "mcp__plugin_soundings-app-interface_helper__annotate" {
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
