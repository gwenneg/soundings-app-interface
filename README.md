# Soundings for app-interface

**Take soundings before you ship.**

Soundings for app-interface is a [Claude Code](https://claude.com/claude-code)
plugin that reads a Red Hat
[app-interface](https://gitlab.cee.redhat.com/service/app-interface)
deployment merge request before you merge it, the way sailors
[sound the depth](https://en.wikipedia.org/wiki/Depth_sounding) ahead
before committing to a course. Point it at the MR, by IID or URL:

```
/soundings-app-interface:analyze 12345
```

It takes the compare URL of every service the MR bumps from the newest
`devtools-bot` `Diffs:` comment and hands them all to
[Soundings](https://github.com/gwenneg/soundings) in a single analysis.
A few minutes later you get one clear verdict (release, manual review,
or no-go), a report specific enough to act on, and an offer to post it
back to the MR under your own identity. Nothing to deploy, no LLM API
key: it runs in the Claude Code session you already have, with the
`GITLAB_TOKEN` you already use.

> [!IMPORTANT]
> This plugin is for Red Hat associates. It is hardwired to
> `gitlab.cee.redhat.com`, needs the VPN, and only understands
> app-interface's bot-comment conventions. The repository is public
> because nothing in it is secret. To analyze any other GitHub or GitLab
> diff, install [Soundings](https://github.com/gwenneg/soundings)
> directly.

> Read the introduction post to Soundings:
> [Take soundings before you ship](https://gwenneg.com/2026/09/03/take-soundings-before-you-ship.html).

## Why this plugin

- **One command from MR to verdict:** no copying compare URLs out of the
  bot comment.
- **The whole deployment at once:** every service in the MR is analyzed
  together, catching compound risks across repositories that per-repo CI
  never sees.
- **The report goes where the decision happens:** posted to the MR as a
  new comment, under your identity, only after you said yes.
- **Never runs on its own:** most app-interface MRs are not deployment
  MRs, so Claude will not start it because you mentioned one.
- **Safe on untrusted input:** everything read from the MR is treated as
  hostile, and the plugin can do exactly three things with GitLab.

## Quick start

You need [Claude Code](https://claude.com/claude-code), a Go toolchain
(`brew install go`), VPN connectivity to the app-interface GitLab host,
and `GITLAB_TOKEN` set to a personal access token with the `api` scope
for that host. [Soundings](https://github.com/gwenneg/soundings) 0.10.0
or later does the analysis, so install both plugins from the
[claude-ichiba](https://github.com/gwenneg/claude-ichiba) marketplace:

```
/plugin marketplace add gwenneg/claude-ichiba
/plugin install soundings@claude-ichiba
/plugin install soundings-app-interface@claude-ichiba
/reload-plugins
```

Then point it at a deployment MR:

```
/soundings-app-interface:analyze 12345
```

The session shows the report's opening section: the summary, the
recommendation, what drove it, and the override banner when one
applies. The full report is a Markdown file in your working directory.
The skill then asks whether to post that file to the MR, and answers
with the comment's URL when you say yes. A normal run needs no
permission prompts.

Update both plugins together, since a new release of this plugin can
rely on a Soundings fix an older copy lacks: enable auto-update for
claude-ichiba in the `/plugin` panel, or run
`/plugin marketplace update claude-ichiba` and `/reload-plugins`.

## Usage

| Input | Example |
|-------|---------|
| MR IID | `/soundings-app-interface:analyze 12345` |
| MR URL | `/soundings-app-interface:analyze https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/12345` |

Only deployment MRs work: the plugin needs the `Diffs:` comment
`devtools-bot` leaves on them, and stops with a clear message otherwise.

Re-running after new commits is safe and intended. The resolver reads
the MR's current state, and each run posts a new comment instead of
editing the previous one, so the MR keeps the history of how the
verdict evolved.

## What you get

<details>
<summary>Top of a report (from the demo analysis of a two-service deployment)</summary>

```markdown
**⚠️ AI-Generated Report** — This report is AI-generated and advisory. Always review AI-generated content prior to use.

# 🚀 Release Readiness Report

## 🎯 Summary

Multi-service release of soundings-demo-api and soundings-demo-gateway with a database migration, email connector changes, and significant API changes requiring careful deployment coordination

**Recommendation:** 🚫 **RELEASE NOT RECOMMENDED**

Driven by 1 critical concern — detailed in Risk Analysis below.

**🔓 Override Justification Required** — If you proceed with this release despite this recommendation, post a comment in this merge request using `/soundings override <your justification>`. This creates an audit trail and helps improve the tool.

---

## 🔍 Risk Analysis

### Concerns

| | Details |
|----------|---------|
| 🔥 | Database migration `V35__add_severity_column_on_event_table.sql` adds `severity` column + matching JPA field in `src/main/java/com/gwenneg/soundingsdemo/models/Event.java` - violates critical deployment rule requiring split releases |
| ⚠️ | COMPOUND: retry + timeout. HTTP retry count for email delivery service increased from 2 to 5 in `src/main/java/com/gwenneg/soundingsdemo/connectors/EmailConnector.java` + HTTP timeout per attempt increased from 200ms to 1s in `src/main/resources/application.properties` - each change is individually within the 2s public API SLO but combined worst case may exceed it when the email service is degraded |

### Positive Factors
- New bulk export endpoint gated behind feature flag `FEATURE_BULK_EXPORT` in `src/main/java/com/gwenneg/soundingsdemo/config/FeatureConfig.java` - can be disabled without redeployment
- `soundings-demo-gateway` event routing alignment in PR #342 was verified compatible with both old and new `soundings-demo-api` versions

---

## 📋 Action Items

### 🔥 Critical (Complete Before Release)
- BLOCK DEPLOYMENT: Split release into two parts - deploy SQL migration `V35` first, then deploy code changes with `severity` field in separate release
```

</details>

This plugin adds the override banner: whoever merges against a no-go
verdict is asked to leave a `/soundings override <justification>`
comment on the MR, for the audit trail. Everything else above comes
from Soundings.
The critical concern caught a migration and the code using its new
column shipping together, against the service's own deployment rules.
The compound one connected a retry count raised in one file with a
timeout raised in another. The last positive factor is only visible
because both services were analyzed together.

Your session shows the opening section, banner included. The file adds
action items in three urgency levels, every `/soundings note` with its
author and whether it was used, a changelog per service, and, when
configured, a feedback link. Read the
[full demo report](docs/DEMO_REPORT.md).

## How the verdict is computed

Soundings derives the verdict from a fixed policy: by default any
critical concern blocks the release and any high concern requires manual
review. Set `SOUNDINGS_BLOCK_ON` to `high` or `medium` to tighten that
for critical services. A verdict that feels wrong traces back to one
named concern you can go verify. The policy table is in the Soundings
README, under
[How the verdict is computed](https://github.com/gwenneg/soundings#how-the-verdict-is-computed).

## Getting better results

- **A `.soundings.md` file at the root of each service repository**
  gives the analysis the service's SLOs, deployment rules such as
  "migrations ship in their own release", and rollback procedure. That
  is where the demo's blocking concern came from. Start from Soundings'
  [`.soundings.example.md`](https://github.com/gwenneg/soundings/blob/main/.soundings.example.md).
- **A `/soundings note` comment on the deployment MR** hands the analysis
  context a diff cannot show, such as a load test that already ran. Only
  the MR author's notes are used. Everyone else's are listed in the
  report and ignored. Notes on the services' own PRs and MRs are picked
  up by Soundings as usual.

The full guide is Soundings'
[Improving your release readiness analysis](https://github.com/gwenneg/soundings/blob/main/docs/IMPROVING_ANALYSIS.md).

## Configuration

Set these in the environment Claude Code starts from.

| Env var | Meaning | Default |
|---------|---------|---------|
| `GITLAB_TOKEN` | Personal access token with the `api` scope for the app-interface host. Required. | none |
| `APP_INTERFACE_HOST` | The app-interface GitLab host, and the only host the token is ever sent to. | `gitlab.cee.redhat.com` |
| `SOUNDINGS_BLOCK_ON` | Severity at or above which a concern blocks the release: `critical`, `high`, or `medium`. One level below means manual review. | `critical` |
| `SOUNDINGS_FEEDBACK_URL` | Feedback link inserted at the bottom of every report. | none |
| `SOUNDINGS_LOG_LEVEL` | Helper log level: `debug`, `info`, `warn`, or `error`. | `info` |

## Security

Any app-interface contributor can comment on an MR, so everything the
plugin reads is treated as data, never as instructions, and the
protections are limits the model cannot cross. During a run the skill
has no shell, file, or network tools. All it can do is call three tools
served by a small Go helper bundled as an MCP server:

| Tool | What it does | What it can never do |
|------|--------------|----------------------|
| `resolve` | Lists the MR's notes and extracts the compare URLs and guidance | Write anything, anywhere |
| `annotate` | Inserts the override banner and feedback link into the local report file | Touch the MR |
| `post` | Creates one new MR comment from the report file, after you said yes | Edit or delete a comment |

- **Your token stays on one host.** An MR URL naming any other host is
  rejected before a request is made, and TLS is always verified against
  the system trust store, with no option to skip.
- **Posting is opt-in, every time.** The skill asks you in plain language
  before calling `post`. For a harness prompt on top, add the tool to
  your `ask` rules, which always win over the plugin's own pre-approval.
- **Your token, your identity.** The helper runs under your own token,
  with no service account, LLM endpoint, or telemetry, and reads no
  credential and opens no connection until a tool is invoked.
- **The analysis is isolated.** Fetching and reading the diffs happen in
  Soundings, whose read-only subagent cannot run commands or see your
  files. The [Soundings security model](https://github.com/gwenneg/soundings#security)
  has the full picture, including the residual risk that makes every
  report advisory.

## Troubleshooting

The helper says what went wrong:

| Message | Fix |
|---------|-----|
| `cannot reach <host> - are you on the VPN?` | Connect to the VPN. |
| `TLS verification for <host> failed` | Install the host's CA in your system trust store. |
| `GITLAB_TOKEN is not set` or `authentication to <host> failed` | Create a personal access token with the `api` scope on the host and export it as `GITLAB_TOKEN`. |
| `no "Diffs:" comment from devtools-bot found on this MR` | Not a deployment MR, or the bot has not commented yet. |

Set `SOUNDINGS_LOG_LEVEL=debug` to see the helper's log. Errors from the
analysis itself are covered by Soundings'
[Troubleshooting](https://github.com/gwenneg/soundings/blob/main/docs/TROUBLESHOOTING.md).

## How it relates to Soundings

Everything app-interface-specific lives here: resolving the MR into
compare URLs and guidance, the MR-author authorization rule, the
override banner, the feedback link, and posting. The analysis itself,
from fetching to the rendered report, is entirely Soundings, which never
knows the inputs came from app-interface.

## Development

Run `go test ./...`. Commit messages follow
[Conventional Commits](https://www.conventionalcommits.org/): the release
workflow, shared with Soundings, derives the next version from them, and
merging the standing release PR publishes to the marketplace.

## Provenance and license

This plugin is the successor to the app-interface mode of
[Release Confidence Score](https://github.com/RedHatInsights/release-confidence-score),
originally built at Red Hat. The analysis core became
[Soundings](https://github.com/gwenneg/soundings). Licensed under
[Apache-2.0](LICENSE).
