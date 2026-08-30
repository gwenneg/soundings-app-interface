package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// preApprovedTools is the hook's permission policy: this plugin's own MCP
// tools, by plugin-scoped name, that never raise an interactive prompt.
// post is included by design - posting is still gated by the skill's
// explicit in-session question, and user deny/ask rules can restore a
// harness prompt for any of these tools.
var preApprovedTools = map[string]bool{
	"mcp__plugin_soundings-app-interface_helper__resolve":  true,
	"mcp__plugin_soundings-app-interface_helper__annotate": true,
	"mcp__plugin_soundings-app-interface_helper__post":     true,
}

// runHook is the plugin's PreToolUse hook: emit "allow" for the tools in
// preApprovedTools, nothing for anything else. The skill's allowed-tools
// frontmatter is not a permission grant and a plugin cannot ship
// permission rules, so this hook is what makes a review run prompt-free.
// The hooks.json matcher routes only these tools here, but the exact-name
// check is the security boundary - matcher regex anchoring is
// undocumented, and a matcher edit must not widen the policy.
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
