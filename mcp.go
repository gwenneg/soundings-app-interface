package main

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runMCP serves the resolve, annotate, and post operations as MCP tools
// over stdio. Serving them as named tools instead of Bash subcommands
// means the permission system sees exact tool identities - the review
// skill's turn can disallow Bash entirely, and the plugin's hook
// pre-approves resolve and annotate by name with no command-string
// parsing.
//
// Design rule (same as the soundings helper): this function does nothing
// but register handlers and wait. No credential lookup, no network access,
// no filesystem writes happen until a tool is actually called - the server
// is inert at rest.
func runMCP() error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "soundings-app-interface",
		Title:   "Soundings app-interface helper",
		Version: pluginVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name: "resolve",
		Description: "Resolve an app-interface MR into soundings inputs: the compare URLs " +
			"from the newest devtools-bot Diffs comment, the MR's /soundings note guidance " +
			"comments (pre-authorized - the MR itself is permission-gated), and any " +
			"configured block_on policy/feedback URL. Read-only: it only lists MR notes, " +
			"authenticated by the user's own GITLAB_TOKEN.",
	}, resolveTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "annotate",
		Description: "Insert app-interface's own additions into a rendered soundings report " +
			"file, in place: the override-justification banner when the recommendation is " +
			"RELEASE NOT RECOMMENDED, and the feedback link when a " +
			"feedback URL is given. Both are no-ops when their condition doesn't apply, and " +
			"idempotent if run twice. Local to that file only - it never touches the MR.",
	}, annotateTool)

	mcp.AddTool(server, &mcp.Tool{
		Name: "post",
		Description: "Post a report markdown file to the MR as a NEW comment - never an " +
			"edit, so re-runs keep an audit trail - under the identity of the user's own " +
			"GITLAB_TOKEN. Returns the posted comment's URL. Outward-facing: only call it " +
			"after the user explicitly agreed to post in this session.",
	}, postTool)

	return server.Run(context.Background(), &mcp.StdioTransport{})
}

type resolveToolInput struct {
	MR string `json:"mr" jsonschema:"app-interface MR IID (a number) or full MR URL"`
}

func resolveTool(ctx context.Context, req *mcp.CallToolRequest, in resolveToolInput) (*mcp.CallToolResult, *resolveOutput, error) {
	if in.MR == "" {
		return nil, nil, errors.New("mr is required: pass an app-interface MR IID or URL")
	}
	out, err := doResolve(in.MR)
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}

type annotateToolInput struct {
	ReportPath  string `json:"report_path" jsonschema:"path to the rendered soundings report markdown file to annotate in place"`
	FeedbackURL string `json:"feedback_url,omitempty" jsonschema:"feedback link to insert; only when the resolver emitted one"`
}

type annotateToolOutput struct {
	Modified bool `json:"modified" jsonschema:"whether the file was changed (false when no annotation condition applied or the annotations were already present)"`
}

func annotateTool(ctx context.Context, req *mcp.CallToolRequest, in annotateToolInput) (*mcp.CallToolResult, *annotateToolOutput, error) {
	if in.ReportPath == "" {
		return nil, nil, errors.New("report_path is required")
	}
	modified, err := doAnnotate(in.ReportPath, in.FeedbackURL)
	if err != nil {
		return nil, nil, err
	}
	return nil, &annotateToolOutput{Modified: modified}, nil
}

type postToolInput struct {
	MR         string `json:"mr" jsonschema:"app-interface MR IID (a number) or full MR URL"`
	ReportPath string `json:"report_path" jsonschema:"path to the report markdown file to post as a new MR comment"`
}

type postToolOutput struct {
	CommentURL string `json:"comment_url" jsonschema:"URL of the posted comment"`
}

func postTool(ctx context.Context, req *mcp.CallToolRequest, in postToolInput) (*mcp.CallToolResult, *postToolOutput, error) {
	if in.MR == "" || in.ReportPath == "" {
		return nil, nil, errors.New("mr and report_path are required")
	}
	url, err := doPost(in.MR, in.ReportPath)
	if err != nil {
		return nil, nil, err
	}
	return nil, &postToolOutput{CommentURL: url}, nil
}
