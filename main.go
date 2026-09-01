// Command soundings-app-interface is the deterministic helper behind the
// soundings-app-interface Claude Code plugin. Two subcommands:
//
//	resolve <MR IID or URL>          Resolve an app-interface MR into
//	                                 soundings inputs: the compare URLs from
//	                                 the newest devtools-bot "Diffs:"
//	                                 comment, the MR's "/soundings note"
//	                                 guidance comments (pre-authorized - the
//	                                 MR itself is permission-gated), and any
//	                                 configured block_on policy/feedback URL.
//	                                 Prints one JSON object.
//
//	post <MR IID or URL> <file>      Post a report markdown file to the MR
//	                                 as a NEW comment - never an edit - so
//	                                 re-runs keep an audit trail. Prints the
//	                                 posted comment's URL.
//
// GitLab access uses the official Go SDK, authenticated by GITLAB_TOKEN -
// the user's own personal access token, so everything happens under their
// identity. TLS is always verified against the operating system's
// certificate store, which is where a corporate CA lives; there is
// deliberately no skip option. The helper only ever reads or writes MR
// notes - fetching diff content stays inside soundings.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

const (
	project     = "service/app-interface"
	botUsername = "devtools-bot"
	diffsMarker = "Diffs:"
	defaultHost = "gitlab.cee.redhat.com"
)

var (
	// URLs on lines starting with "- " inside the bot comment.
	urlRe = regexp.MustCompile(`(?m)^- (https?://\S+)$`)
	// "/soundings note <text>", multiline and case-insensitive.
	guidanceRe = regexp.MustCompile(`(?is)^\s*/soundings\s+note\s+(\S.*\S|\S)`)
	mrURLRe    = regexp.MustCompile(`^https?://([^/]+)/(.+)/-/merge_requests/(\d+)`)
)

func main() {
	var err error
	switch {
	case len(os.Args) == 3 && os.Args[1] == "resolve":
		err = runResolve(os.Args[2])
	case len(os.Args) == 4 && os.Args[1] == "post":
		err = runPost(os.Args[2], os.Args[3])
	default:
		err = errors.New("usage: resolve <MR IID or URL> | post <MR IID or URL> <report markdown file>")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "soundings-app-interface: %v\n", err)
		os.Exit(1)
	}
}

// note is the subset of a GitLab MR note the helper works with; keeping it
// as an internal type keeps the extraction logic and its tests SDK-free.
type note struct {
	ID     int64 `json:"id"`
	Author struct {
		Username string `json:"username"`
	} `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type guidanceEntry struct {
	Content    string `json:"content"`
	Author     string `json:"author"`
	Date       string `json:"date"`
	CommentURL string `json:"comment_url"`
}

type resolveOutput struct {
	MRURL       string          `json:"mr_url"`
	DiffURLs    []string        `json:"diff_urls"`
	Guidance    []guidanceEntry `json:"guidance"`
	FeedbackURL string          `json:"feedback_url,omitempty"`
	BlockOn     string          `json:"block_on,omitempty"`
}

func runResolve(mrArg string) error {
	host, iid, err := parseMRArg(mrArg, envHost())
	if err != nil {
		return err
	}
	mrURL := fmt.Sprintf("https://%s/%s/-/merge_requests/%d", host, project, iid)

	client, err := newGitLabClient(host)
	if err != nil {
		return err
	}
	notes, err := fetchNotes(client, host, iid)
	if err != nil {
		return err
	}
	diffURLs, err := extractDiffURLs(notes)
	if err != nil {
		return err
	}
	out := resolveOutput{MRURL: mrURL, DiffURLs: diffURLs, Guidance: extractGuidance(notes, mrURL)}
	out.FeedbackURL = os.Getenv("SOUNDINGS_FEEDBACK_URL")
	if out.BlockOn, err = envBlockOn("SOUNDINGS_BLOCK_ON"); err != nil {
		return err
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func runPost(mrArg, reportPath string) error {
	host, iid, err := parseMRArg(mrArg, envHost())
	if err != nil {
		return err
	}
	body, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("cannot read report file: %w", err)
	}
	text := string(body)
	if strings.TrimSpace(text) == "" {
		return errors.New("report file is empty - refusing to post an empty comment")
	}

	client, err := newGitLabClient(host)
	if err != nil {
		return err
	}
	posted, _, err := client.Notes.CreateMergeRequestNote(project, iid,
		&gitlab.CreateMergeRequestNoteOptions{Body: &text})
	if err != nil {
		return fmt.Errorf("posting comment failed: %w", classifyErr(host, err))
	}
	fmt.Printf("https://%s/%s/-/merge_requests/%d#note_%d\n", host, project, iid, posted.ID)
	return nil
}

// newGitLabClient builds an SDK client for one host, authenticated by the
// user's own GITLAB_TOKEN. TLS is always verified - there is deliberately
// no skip option.
func newGitLabClient(host string) (*gitlab.Client, error) {
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN is not set - create a personal access token (api scope) on https://%s and export it as GITLAB_TOKEN", host)
	}
	// The default transport verifies TLS against the OS trust store.
	return gitlab.NewClient(token,
		gitlab.WithBaseURL("https://"+host),
		gitlab.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}))
}

// parseMRArg accepts a bare IID or a full MR URL; returns (host, iid).
func parseMRArg(arg, defaultHost string) (string, int64, error) {
	if m := mrURLRe.FindStringSubmatch(arg); m != nil {
		if m[2] != project {
			return "", 0, fmt.Errorf("expected an MR of %s, got %s", project, m[2])
		}
		iid, err := strconv.ParseInt(m[3], 10, 64)
		if err != nil {
			return "", 0, fmt.Errorf("invalid MR IID %q", m[3])
		}
		return strings.ToLower(m[1]), iid, nil
	}
	if iid, err := strconv.ParseInt(arg, 10, 64); err == nil && iid > 0 {
		return defaultHost, iid, nil
	}
	return "", 0, fmt.Errorf("expected an MR IID or MR URL, got %q", arg)
}

// fetchNotes lists all MR notes, newest first, with pagination.
func fetchNotes(client *gitlab.Client, host string, iid int64) ([]note, error) {
	orderBy, sortDir := "created_at", "desc"
	opts := &gitlab.ListMergeRequestNotesOptions{
		// PerPage 100 is the API maximum - fewest round trips.
		ListOptions: gitlab.ListOptions{PerPage: 100},
		OrderBy:     &orderBy,
		Sort:        &sortDir,
	}
	var notes []note
	for {
		page, resp, err := client.Notes.ListMergeRequestNotes(project, iid, opts)
		if err != nil {
			return nil, classifyErr(host, err)
		}
		for _, n := range page {
			notes = append(notes, fromSDK(n))
		}
		if resp.NextPage == 0 {
			return notes, nil
		}
		opts.Page = resp.NextPage
	}
}

func fromSDK(n *gitlab.Note) note {
	var out note
	out.ID = int64(n.ID)
	out.Body = n.Body
	out.Author.Username = n.Author.Username
	if n.CreatedAt != nil {
		out.CreatedAt = n.CreatedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// classifyErr distinguishes the failure modes the skill relays to the
// user: VPN down, bad token, missing CA, and missing MR.
func classifyErr(host string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "connection refused"):
		return fmt.Errorf("cannot reach %s - are you on the VPN? (%v)", host, err)
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"):
		return fmt.Errorf("TLS verification for %s failed - install its CA in the system trust store (%v)", host, err)
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"):
		return fmt.Errorf("authentication to %s failed - check that GITLAB_TOKEN is valid and has the api scope (%v)", host, err)
	case strings.Contains(msg, "404"):
		return fmt.Errorf("merge request not found on %s/%s (%v)", host, project, err)
	}
	return err
}

// extractDiffURLs returns the URLs from the newest devtools-bot Diffs
// comment (notes arrive newest first).
func extractDiffURLs(notes []note) ([]string, error) {
	for _, n := range notes {
		if n.Author.Username != botUsername || !strings.HasPrefix(n.Body, diffsMarker) {
			continue
		}
		var urls []string
		for _, m := range urlRe.FindAllStringSubmatch(n.Body, -1) {
			urls = append(urls, m[1])
		}
		if len(urls) > 0 {
			return urls, nil
		}
	}
	return nil, fmt.Errorf("no %q comment from %s found on this MR - either it is not a deployment MR, or the bot has not commented yet",
		diffsMarker, botUsername)
}

// extractGuidance collects /soundings note comments, oldest first.
func extractGuidance(notes []note, mrURL string) []guidanceEntry {
	guidance := []guidanceEntry{}
	for _, n := range notes {
		m := guidanceRe.FindStringSubmatch(n.Body)
		if m == nil || n.CreatedAt == "" {
			continue
		}
		guidance = append(guidance, guidanceEntry{
			Content:    m[1],
			Author:     n.Author.Username,
			Date:       n.CreatedAt,
			CommentURL: fmt.Sprintf("%s#note_%d", mrURL, n.ID),
		})
	}
	// Notes arrive newest first; the report reads best oldest first.
	for i, j := 0, len(guidance)-1; i < j; i, j = i+1, j-1 {
		guidance[i], guidance[j] = guidance[j], guidance[i]
	}
	return guidance
}

func envHost() string {
	if h := os.Getenv("APP_INTERFACE_HOST"); h != "" {
		return h
	}
	return defaultHost
}

// envBlockOn reads the soundings block_on policy - the severity at or
// above which a concern blocks the release - from the named env var.
// Empty means "let soundings use its default".
func envBlockOn(name string) (string, error) {
	switch v := os.Getenv(name); v {
	case "", "critical", "high", "medium":
		return v, nil
	default:
		return "", fmt.Errorf("%s must be one of critical, high, medium, got %q", name, v)
	}
}
