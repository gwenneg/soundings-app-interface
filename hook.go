package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// runHook implements the plugin's PreToolUse hook: pre-approve Bash
// invocations of this plugin's own read-only or file-local helper
// subcommands (resolve, annotate) so a review run needs no Bash prompts.
// The skill's allowed-tools frontmatter constrains what the turn may use
// but is not a permission grant, and a plugin cannot ship permission
// rules - an explicit hook "allow" is the plugin-side mechanism that
// skips the prompt. User-configured deny and ask rules still apply
// regardless of the allow.
//
// post is deliberately NOT pre-approved: it is the one outward-facing
// action (it posts the report to the MR under the user's identity), so
// its interactive prompt stays as a harness-level gate on top of the
// skill's ask-before-posting instruction.
//
// The command must be exactly a plain helper invocation: any shell
// metacharacter (chaining, substitution, redirection, quoting)
// disqualifies it, so nothing can ride along with an approved prefix.
// Everything else gets no opinion, leaving the session's normal
// permission flow untouched.
func runHook(stdin io.Reader, stdout io.Writer, pluginRoot string) error {
	var in struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return fmt.Errorf("parsing hook input: %w", err)
	}
	if in.ToolName != "Bash" || !isOwnHelperCommand(in.ToolInput.Command, pluginRoot) {
		return nil
	}
	return json.NewEncoder(stdout).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "allow",
			"permissionDecisionReason": "soundings-app-interface: the plugin's own helper command is pre-approved",
		},
	})
}

// shellMeta are characters that would let a command chain, substitute,
// redirect, quote, or comment - none of which a plain helper invocation
// contains. A command using any of them is not recognized and falls back
// to the normal permission flow.
const shellMeta = ";&|`$(){}<>#!\n\r\\\"'*?~"

// isOwnHelperCommand reports whether command is exactly
// "go -C <pluginRoot> run . resolve <arg>" or
// "go -C <pluginRoot> run . annotate <arg> [<arg>]".
func isOwnHelperCommand(command, pluginRoot string) bool {
	if pluginRoot == "" || strings.ContainsAny(command, shellMeta) {
		return false
	}
	fields := strings.Fields(command)
	prefix := []string{"go", "-C", pluginRoot, "run", "."}
	if len(fields) < len(prefix)+2 {
		return false
	}
	for i, p := range prefix {
		if fields[i] != p {
			return false
		}
	}
	switch fields[len(prefix)] {
	case "resolve":
		return len(fields) == len(prefix)+2
	case "annotate":
		return len(fields) == len(prefix)+2 || len(fields) == len(prefix)+3
	}
	return false
}
