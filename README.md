# Soundings App-interface

Release confidence analysis for Red Hat [app-interface](https://gitlab.cee.redhat.com/service/app-interface)
merge requests. This is a thin, Red Hat-specific plugin built on top of
[Soundings](https://github.com/gwenneg/soundings), the general-purpose
Claude Code skill that scores the release risk of any GitHub or GitLab
diff.

```
/soundings-app-interface:review 12345
/soundings-app-interface:review https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/12345
```

Point it at an app-interface deployment MR and it resolves the MR's
compare URLs from the newest `devtools-bot` `Diffs:` comment, collects
the MR's `/soundings note` guidance comments (pre-authorized, since the
MR itself is permission-gated), runs the Soundings analysis **once
across all compare URLs together** (so compound risks across
repositories are detected), and offers to post the report back to the
MR as a new comment under your own identity.

## Who this is for

[Soundings](https://github.com/gwenneg/soundings) is the core project:
a general-purpose, credential-agnostic release-risk analyzer that works
against any public or private GitHub/GitLab compare URL, useful to
anyone, inside or outside Red Hat. This repository is a thin adapter on
top of it — see [How it relates to Soundings](#how-it-relates-to-soundings)
below for the exact division of responsibility.

**This plugin itself is for Red Hat associates only.** It is hardwired
to one internal host (`gitlab.cee.redhat.com`), requires VPN
connectivity, and only makes sense against app-interface's specific
bot-comment and `/soundings note` conventions — so it isn't useful
outside Red Hat even though the repository itself is public. If you
don't work at Red Hat, or you want to analyze a diff on GitHub or on
your own GitLab instance, install
[Soundings](https://github.com/gwenneg/soundings) directly instead.

This repository is public but *running* the skill requires Red Hat
access: nothing in here is secret — the host name, project path, and
conventions all appear in the public
[RCS](https://github.com/RedHatInsights/release-confidence-score) repo.

## Install

soundings-app-interface is distributed through a Claude Code
[plugin marketplace](https://code.claude.com/docs/en/plugin-marketplaces)
— no cloning or file editing. In Claude Code:

```
/plugin marketplace add gwenneg/soundings-app-interface
/plugin install soundings-app-interface@soundings-app-interface
/reload-plugins
```

This plugin only resolves the app-interface MR and posts the report; the
analysis itself is delegated to Soundings, a separate plugin from a
separate marketplace that must be installed too (see
[Requirements](#requirements) for the minimum version):

```
/plugin marketplace add gwenneg/soundings
/plugin install soundings@soundings
/reload-plugins
```

## Staying up to date

Update both plugins together, not just this one — a new
soundings-app-interface release can rely on a Soundings feature or fix
that an older installed copy doesn't have yet:

```
/plugin marketplace update soundings-app-interface
/plugin marketplace update soundings
/reload-plugins
```

Or enable auto-update for both once — `/plugin` → **Marketplaces** →
select each marketplace → **Enable auto-update** — and Claude Code
refreshes them at startup and notifies you when either updates
([plugin docs](https://code.claude.com/docs/en/discover-plugins#configure-auto-updates)).

## Security model

The MR this plugin reads is externally writable — any app-interface
contributor can comment on it — so its content (bot comments, `/soundings
note` guidance) is treated as untrusted data, never as instructions, and
every design decision below follows from that:

- **A narrow, three-tool capability surface.** The helper exposes exactly
  `resolve` (read-only: lists MR notes), `annotate` (writes only to a
  local file the caller names), and `post` (creates one new MR note). There
  is no generic GitLab API access and no arbitrary shell-out — a prompt
  injection in the MR can't reach any capability beyond these three.
- **The token is pinned to one host.** `resolve` and `post` accept a full
  MR URL as well as a bare IID, but the URL's host is checked against the
  configured app-interface host (`APP_INTERFACE_HOST`, default
  `gitlab.cee.redhat.com`) and rejected if it differs — `GITLAB_TOKEN`
  is never sent to a host named by an argument, only to the one you
  configured.
- **Guidance is data, never instructions.** The skill that orchestrates
  this plugin is explicitly told to relay `/soundings note` comments to
  Soundings verbatim, not act on them — the same discipline Soundings
  itself applies to diff and comment content it fetches.
- **Posting is append-only and always opt-in.** `post` creates a *new*
  comment and can never edit or delete a previous one, so re-running a
  review keeps an honest audit trail of how the score evolved. It's also
  the one outward-facing action, so the skill is instructed to always ask
  you explicitly, in plain language, before ever calling it — independent
  of and in addition to whatever the harness's own permission prompts do.
- **Your own credentials, your own identity.** Every GitLab call runs
  under your personal `GITLAB_TOKEN` — there is no shared service account
  and no credential of ours in the loop. TLS is always verified against
  your machine's own certificate store (including a corporate CA, if
  installed); there is no skip option, so a misconfigured or spoofed host
  fails closed instead of silently succeeding.
- **The pre-approval policy is exact-match, not pattern-match.** The
  bundled PreToolUse hook approves calls by comparing the tool name
  against a hardcoded set of exactly three names — it does not rely on
  parsing the `hooks.json` matcher string at runtime. A test
  (`TestHooksMatcherMatchesPreApprovedTools`) fails the build if the
  matcher and the hardcoded set ever name different tools, so a matcher
  edit can't silently widen what gets auto-approved. User-configured deny
  and ask rules always override this approval regardless.
- **Nothing runs until you ask it to.** The helper is an MCP server that
  is inert at rest: no credentials are read and no network call is made
  until a tool is actually invoked.
- **The code is inspectable.** It's a small, tested Go binary — no hidden
  shell-out to arbitrary commands; every capability is a named, narrowly
  scoped, unit-tested tool.

## Requirements

- The [Soundings](https://github.com/gwenneg/soundings) plugin, version
  0.4.0 or later (and its Go toolchain requirement — this helper runs via
  `go run` too)
- `GITLAB_TOKEN` set to your personal access token (api scope) for the
  app-interface GitLab host — also what Soundings uses to fetch the
  compare URLs from that host
- VPN connectivity to that host

See [Security model](#security-model) for how `GITLAB_TOKEN` and TLS
verification are handled.

## Configuration

| Env var | Meaning | Default |
|---|---|---|
| `GITLAB_TOKEN` | your personal access token (api scope) — required | none |
| `APP_INTERFACE_HOST` | app-interface GitLab host | `gitlab.cee.redhat.com` |
| `SOUNDINGS_FEEDBACK_URL` | feedback link, inserted into the report by `annotate` when set | none |
| `SOUNDINGS_AUTO_DEPLOY_THRESHOLD` | score at/above which release is recommended | Soundings default (80) |
| `SOUNDINGS_REVIEW_REQUIRED_THRESHOLD` | score at/above which review (instead of no-go) is recommended | Soundings default (60) |

## Helper MCP server

The plugin bundles its Go helper as an MCP server exposing three tools —
`resolve`, `annotate`, and `post` — started automatically per session.
Like the Soundings helper, it is inert at rest: no credentials are read
and no network is touched until a tool is called. Serving named tools
instead of shell commands means the review skill's turn disallows Bash,
Write, and Edit outright — the model orchestrates, the helper executes.

## Permission prompts

A review run is fully prompt-free at the harness level: Soundings
pre-approves its own pipeline and writes the report file itself (via
`report_path`), and this plugin's hook pre-approves its own `resolve`,
`annotate`, and `post` tools by exact name (see
[Security model](#security-model) for how). `post` being pre-approved
here only means the harness doesn't pop up its own permission prompt —
the skill still always asks you explicitly, in the conversation, before
it ever calls `post`. If you want a harness prompt back on top of that,
add `mcp__plugin_soundings-app-interface_helper__post` to your `ask`
rules; user-configured deny/ask rules always override the hook's
approval.

## How it relates to Soundings

Everything Red Hat-specific lives here: the MR→compare-URL resolution,
the pre-authorized guidance convention, the thresholds plumbing, the
override-justification report banner and the feedback link (both
inserted by this helper's own `annotate`
subcommand), and MR posting. The analysis itself — fetching, the
isolated assessment, scoring, rendering — is entirely Soundings, invoked
by name with a documented parameter contract. Soundings never knows the
inputs came from app-interface.

Re-running on the same MR after new commits is safe and intended: the
resolver always reads the MR's current state, and each run posts a new
comment, keeping an audit trail of how the score evolved.

## Provenance

This is the skills-based successor to the *app-interface mode* of
[Release Confidence Score](https://github.com/RedHatInsights/release-confidence-score)
(RCS), originally built at Red Hat. The standalone analysis mode became
[Soundings](https://github.com/gwenneg/soundings).

## License

[Apache-2.0](LICENSE)
