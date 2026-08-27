// Command soundings-app-interface is the deterministic helper behind the
// soundings-app-interface Claude Code plugin. Two subcommands:
//
//	resolve <MR IID or URL>          Resolve an app-interface MR into
//	                                 soundings inputs: the compare URLs from
//	                                 the newest devtools-bot "Diffs:"
//	                                 comment, the MR's "/soundings note"
//	                                 guidance comments (pre-authorized - the
//	                                 MR itself is permission-gated), and any
//	                                 configured thresholds/feedback URL.
//	                                 Prints one JSON object.
//
//	post <MR IID or URL> <file>      Post a report markdown file to the MR
//	                                 as a NEW comment - never an edit - so
//	                                 re-runs keep an audit trail. Prints the
//	                                 posted comment's URL.
//
// All GitLab access goes through `glab api` under the user's own
// authentication; the helper never handles tokens itself and only ever
// reads or writes MR notes - fetching diff content stays inside soundings.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
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
	MRURL          string          `json:"mr_url"`
	DiffURLs       []string        `json:"diff_urls"`
	Guidance       []guidanceEntry `json:"guidance"`
	FeedbackURL    string          `json:"feedback_url,omitempty"`
	AutoDeploy     *int            `json:"auto_deploy,omitempty"`
	ReviewRequired *int            `json:"review_required,omitempty"`
}

func runResolve(mrArg string) error {
	host, iid, err := parseMRArg(mrArg, envHost())
	if err != nil {
		return err
	}
	mrURL := fmt.Sprintf("https://%s/%s/-/merge_requests/%d", host, project, iid)

	notes, err := fetchNotes(host, iid)
	if err != nil {
		return err
	}
	diffURLs, err := extractDiffURLs(notes)
	if err != nil {
		return err
	}
	out := resolveOutput{MRURL: mrURL, DiffURLs: diffURLs, Guidance: extractGuidance(notes, mrURL)}
	out.FeedbackURL = os.Getenv("APP_INTERFACE_FEEDBACK_URL")
	if out.AutoDeploy, err = envThreshold("APP_INTERFACE_AUTO_DEPLOY_THRESHOLD"); err != nil {
		return err
	}
	if out.ReviewRequired, err = envThreshold("APP_INTERFACE_REVIEW_REQUIRED_THRESHOLD"); err != nil {
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
	if strings.TrimSpace(string(body)) == "" {
		return errors.New("report file is empty - refusing to post an empty comment")
	}

	path := fmt.Sprintf("projects/%s/merge_requests/%d/notes", escapedProject(), iid)
	stdout, err := glabAPI(host, "-X", "POST", path, "-f", "body="+string(body))
	if err != nil {
		return fmt.Errorf("posting comment failed: %w", err)
	}
	var posted note
	if err := json.Unmarshal(stdout, &posted); err != nil {
		return fmt.Errorf("unexpected response from glab: %w", err)
	}
	fmt.Printf("https://%s/%s/-/merge_requests/%d#note_%d\n", host, project, iid, posted.ID)
	return nil
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

// fetchNotes lists all MR notes, newest first, via glab api with pagination.
func fetchNotes(host string, iid int64) ([]note, error) {
	path := fmt.Sprintf("projects/%s/merge_requests/%d/notes?per_page=100&order_by=created_at&sort=desc",
		escapedProject(), iid)
	stdout, err := glabAPI(host, "--paginate", path)
	if err != nil {
		return nil, err
	}
	return parseConcatenatedArrays(bytes.NewReader(stdout))
}

// glabAPI runs `glab api --hostname <host> <args>` and classifies failures
// so the skill can relay "VPN down" vs "auth" vs everything else.
func glabAPI(host string, args ...string) ([]byte, error) {
	cmd := exec.Command("glab", append([]string{"api", "--hostname", host}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("glab is not installed - install it and run 'glab auth login --hostname %s'", host)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		switch {
		case strings.Contains(msg, "no such host"), strings.Contains(msg, "dial tcp"), strings.Contains(msg, "i/o timeout"):
			return nil, fmt.Errorf("cannot reach %s - are you on the VPN? (%s)", host, msg)
		case strings.Contains(msg, "401"), strings.Contains(msg, "403"), strings.Contains(strings.ToLower(msg), "authenticate"):
			return nil, fmt.Errorf("authentication to %s failed - run 'glab auth login --hostname %s' (%s)", host, host, msg)
		}
		return nil, fmt.Errorf("glab api failed: %s", msg)
	}
	return stdout.Bytes(), nil
}

// parseConcatenatedArrays handles `glab api --paginate` output: one JSON
// array per page, concatenated back to back.
func parseConcatenatedArrays(r io.Reader) ([]note, error) {
	var notes []note
	dec := json.NewDecoder(r)
	for {
		var page []note
		if err := dec.Decode(&page); err == io.EOF {
			return notes, nil
		} else if err != nil {
			return nil, fmt.Errorf("parsing glab api output: %w", err)
		}
		notes = append(notes, page...)
	}
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

func escapedProject() string {
	return strings.ReplaceAll(project, "/", "%2F")
}

func envHost() string {
	if h := os.Getenv("APP_INTERFACE_HOST"); h != "" {
		return h
	}
	return defaultHost
}

func envThreshold(name string) (*int, error) {
	v := os.Getenv(name)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 100 {
		return nil, fmt.Errorf("%s must be an integer between 0 and 100, got %q", name, v)
	}
	return &n, nil
}
