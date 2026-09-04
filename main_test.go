package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v2"
)

func mkNote(body, author string, id int64, createdAt string) note {
	var n note
	n.ID = id
	n.Body = body
	n.Author.Username = author
	n.CreatedAt = createdAt
	return n
}

const botBody = "Diffs:\n- https://github.com/org/app/compare/v1...v2\n- https://gitlab.cee.redhat.com/g/p/-/compare/a...b\nnot a url line"

func TestParseMRArg(t *testing.T) {
	cases := []struct {
		name        string
		arg         string
		allowedHost string
		wantHost    string
		wantIID     int64
		wantErr     bool
	}{
		{"bare iid", "12345", "h.example.com", "h.example.com", 12345, false},
		{"full url on allowed host", "https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/98765", "gitlab.cee.redhat.com", "gitlab.cee.redhat.com", 98765, false},
		{"full url on allowed host, different case", "https://GitLab.cee.redhat.com/service/app-interface/-/merge_requests/98765", "gitlab.cee.redhat.com", "gitlab.cee.redhat.com", 98765, false},
		{"full url on a different host is rejected", "https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/98765", "h.example.com", "", 0, true},
		{"wrong project", "https://gitlab.cee.redhat.com/other/repo/-/merge_requests/1", "gitlab.cee.redhat.com", "", 0, true},
		{"not a number", "not-a-number", "h.example.com", "", 0, true},
		{"negative", "-3", "h.example.com", "", 0, true},
	}
	for _, c := range cases {
		host, iid, err := parseMRArg(c.arg, c.allowedHost)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if err == nil && (host != c.wantHost || iid != c.wantIID) {
			t.Errorf("%s: got (%q, %d), want (%q, %d)", c.name, host, iid, c.wantHost, c.wantIID)
		}
	}
}

func TestExtractDiffURLsNewestBotCommentWins(t *testing.T) {
	notes := []note{ // newest first, as the API returns them
		mkNote("Diffs:\n- https://github.com/org/app/compare/v2...v3", botUsername, 9, "2026-08-27T11:00:00Z"),
		mkNote(botBody, botUsername, 1, "2026-08-27T10:00:00Z"),
	}
	urls, err := extractDiffURLs(notes)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://github.com/org/app/compare/v2...v3" {
		t.Fatalf("got %v", urls)
	}
}

func TestExtractDiffURLsIgnoresNonBotAndNonDiffs(t *testing.T) {
	notes := []note{
		mkNote("Diffs:\n- https://github.com/evil/evil/compare/a...b", "attacker", 3, "2026-08-27T10:00:00Z"),
		mkNote("looks normal", botUsername, 2, "2026-08-27T10:00:00Z"),
		mkNote(botBody, botUsername, 1, "2026-08-27T10:00:00Z"),
	}
	urls, err := extractDiffURLs(notes)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected the 2 URLs from the real bot comment, got %v", urls)
	}
}

func TestExtractDiffURLsNoBotComment(t *testing.T) {
	if _, err := extractDiffURLs([]note{mkNote("hello", "someone", 1, "2026-08-27T10:00:00Z")}); err == nil {
		t.Fatal("expected an error when no devtools-bot Diffs comment exists")
	}
}

func TestExtractGuidance(t *testing.T) {
	notes := []note{ // newest first
		mkNote("/soundings note newer guidance", "alice", 3, "2026-08-27T11:00:00Z"),
		mkNote("/SOUNDINGS NOTE older\nspanning lines", "bob", 2, "2026-08-27T10:00:00Z"),
		mkNote("just a comment", "carol", 1, "2026-08-27T09:00:00Z"),
	}
	g := extractGuidance(notes, "https://h/mr/1", "bob")
	if len(g) != 2 {
		t.Fatalf("got %d entries", len(g))
	}
	if g[0].Author != "bob" || g[0].Content != "older\nspanning lines" {
		t.Errorf("oldest first expected, got %+v", g[0])
	}
	if g[1].CommentURL != "https://h/mr/1#note_3" {
		t.Errorf("got comment url %q", g[1].CommentURL)
	}
	// Only the MR author's note is authorized by code.
	if !g[0].IsAuthorized || g[1].IsAuthorized {
		t.Errorf("expected bob (MR author) authorized and alice not, got %+v", g)
	}
}

func TestExtractGuidanceUnknownAuthorAuthorizesNobody(t *testing.T) {
	g := extractGuidance([]note{mkNote("/soundings note x", "bob", 1, "2026-08-27T10:00:00Z")}, "u", "")
	if len(g) != 1 || g[0].IsAuthorized {
		t.Fatalf("with no MR author, no note may be authorized, got %+v", g)
	}
}

func TestExtractGuidanceIgnoresLegacyRCSNote(t *testing.T) {
	if g := extractGuidance([]note{mkNote("/rcs note old convention", "bob", 1, "2026-08-27T10:00:00Z")}, "u", "bob"); len(g) != 0 {
		t.Fatalf("legacy /rcs note must be ignored, got %+v", g)
	}
}

func TestExtractGuidanceSkipsMissingCreatedAt(t *testing.T) {
	if g := extractGuidance([]note{mkNote("/soundings note x", "bob", 1, "")}, "u", "bob"); len(g) != 0 {
		t.Fatalf("expected no entries, got %+v", g)
	}
}

func TestAnnotateReportInsertsBannerWhenNotRecommended(t *testing.T) {
	report := "**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**\n\nSummary text.\n\n---\n\n## Risk Analysis\n"
	got := annotateReport(report, "")
	if !strings.Contains(got, overrideBanner) {
		t.Fatalf("expected banner to be inserted, got %q", got)
	}
	if bannerIdx, sepIdx := strings.Index(got, overrideBanner), strings.Index(got, "---"); bannerIdx >= sepIdx {
		t.Fatalf("expected banner before the separator, got %q", got)
	}
	// A paragraph immediately followed by "---" on the next line is parsed
	// as a Setext H2 heading in CommonMark, not a thematic break - the
	// banner and the separator must be split by a blank line or the
	// separator silently stops rendering as a horizontal rule.
	if !strings.Contains(got, "tool.\n\n---") {
		t.Fatalf("expected a blank line between the banner and the separator, got %q", got)
	}
}

func TestAnnotateReportLeavesOtherRecommendationsUnchanged(t *testing.T) {
	report := "**Recommendation:** ⚠️ **MANUAL REVIEW REQUIRED**\n\nSummary text.\n\n---\n\n## Risk Analysis\n"
	if got := annotateReport(report, ""); got != report {
		t.Fatalf("expected no change, got %q", got)
	}
}

func TestAnnotateReportIsIdempotent(t *testing.T) {
	report := "**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**\n\nSummary text.\n\n---\n\n## Risk Analysis\n\n---\n\n*🤖 Generated by [Soundings](https://x) | model | now*"
	once := annotateReport(report, "https://forms.gle/feedback")
	twice := annotateReport(once, "https://forms.gle/feedback")
	if once != twice {
		t.Fatalf("expected a second pass to be a no-op, got %q", twice)
	}
}

func TestAnnotateReportNoSeparatorLeavesUnchanged(t *testing.T) {
	report := "**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**\n\nSummary text with no separator line.\n"
	if got := annotateReport(report, ""); got != report {
		t.Fatalf("expected no change without a separator line, got %q", got)
	}
}

func TestAnnotateReportInsertsFeedbackLinkBeforeAttribution(t *testing.T) {
	report := "Summary.\n\n---\n\n*🤖 Generated by [Soundings](https://github.com/gwenneg/soundings) | model | now*"
	got := annotateReport(report, "https://forms.gle/feedback")
	if !strings.Contains(got, "https://forms.gle/feedback") {
		t.Fatalf("expected feedback URL to be inserted, got %q", got)
	}
	if linkIdx, attrIdx := strings.Index(got, "Share your feedback"), strings.Index(got, "Generated by"); linkIdx >= attrIdx {
		t.Fatalf("expected feedback link before the attribution line, got %q", got)
	}
}

func TestAnnotateReportNoFeedbackURLLeavesUnchanged(t *testing.T) {
	report := "Summary.\n\n---\n\n*🤖 Generated by [Soundings](https://github.com/gwenneg/soundings) | model | now*"
	if got := annotateReport(report, ""); got != report {
		t.Fatalf("expected no change without a feedback URL, got %q", got)
	}
}

func TestAnnotateReportBannerAndFeedbackCombine(t *testing.T) {
	report := "**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**\n\nSummary text.\n\n---\n\n## Risk Analysis\n\n---\n\n*🤖 Generated by [Soundings](https://github.com/gwenneg/soundings) | model | now*"
	got := annotateReport(report, "https://forms.gle/feedback")
	if !strings.Contains(got, overrideBanner) {
		t.Fatalf("expected banner to be inserted, got %q", got)
	}
	if !strings.Contains(got, "https://forms.gle/feedback") {
		t.Fatalf("expected feedback URL to be inserted, got %q", got)
	}
}

func TestSummarySection(t *testing.T) {
	report := "**banner**\n\n## Summary\n\ntext\n\n**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**\n\n---\n\n## Risk Analysis\n\n---\n"
	want := "**banner**\n\n## Summary\n\ntext\n\n**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**\n"
	if got := summarySection(report); got != want {
		t.Errorf("summarySection = %q, want %q", got, want)
	}
	if got := summarySection("no separator\n"); got != "no separator\n" {
		t.Errorf("a report without a separator should be returned whole, got %q", got)
	}
	// The override banner lands above the first separator, so the summary
	// of an annotated not-recommended report carries it.
	annotated := annotateReport(report, "")
	if got := summarySection(annotated); !strings.Contains(got, "Override Justification Required") {
		t.Errorf("summary of an annotated report should include the override banner, got %q", got)
	}
}

func TestDoAnnotateReturnsSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.md")
	report := "**banner**\n\n**Recommendation:** ✅ ok\n\n---\n\n## Risk Analysis\n\n*🤖 Generated by Soundings*\n"
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, modified, err := doAnnotate(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if modified {
		t.Error("nothing to annotate: modified should be false")
	}
	if want := "**banner**\n\n**Recommendation:** ✅ ok\n"; summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
	summary, modified, err = doAnnotate(path, "https://forms.gle/feedback")
	if err != nil || !modified {
		t.Fatalf("feedback link should modify the file: modified=%v err=%v", modified, err)
	}
	if strings.Contains(summary, "forms.gle") {
		t.Error("the feedback link sits at the end of the report and must not appear in the summary")
	}
}

func TestFromSDK(t *testing.T) {
	created := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	sdkNote := &gitlab.Note{ID: 7, Body: "hello", CreatedAt: &created}
	sdkNote.Author.Username = "alice"
	n := fromSDK(sdkNote)
	if n.ID != 7 || n.Body != "hello" || n.Author.Username != "alice" || n.CreatedAt != "2026-08-27T10:00:00Z" {
		t.Fatalf("got %+v", n)
	}
	if n := fromSDK(&gitlab.Note{ID: 8}); n.CreatedAt != "" {
		t.Fatalf("nil CreatedAt must map to empty string, got %q", n.CreatedAt)
	}
}

func TestEnvBlockOn(t *testing.T) {
	t.Setenv("X_BLOCK_ON", "high")
	v, err := envBlockOn("X_BLOCK_ON")
	if err != nil || v != "high" {
		t.Fatalf("valid severity must survive, got %q, %v", v, err)
	}
	t.Setenv("X_BLOCK_ON", "")
	if v, err := envBlockOn("X_BLOCK_ON"); err != nil || v != "" {
		t.Fatalf("unset must mean the soundings default, got %q, %v", v, err)
	}
	t.Setenv("X_BLOCK_ON", "low")
	if _, err := envBlockOn("X_BLOCK_ON"); err == nil {
		t.Fatal("low is not a valid blocking severity and must fail")
	}
	t.Setenv("X_BLOCK_ON", "80")
	if _, err := envBlockOn("X_BLOCK_ON"); err == nil {
		t.Fatal("a legacy numeric threshold must fail")
	}
}
