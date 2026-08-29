# Soundings App-interface

Release confidence analysis for Red Hat [app-interface](https://gitlab.cee.redhat.com/service/app-interface)
merge requests, built on [soundings](https://github.com/gwenneg/soundings).

```
/soundings-app-interface:review 12345
/soundings-app-interface:review https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/12345
```

The skill resolves the deployment MR's compare URLs from the newest
`devtools-bot` `Diffs:` comment, collects the MR's `/soundings note`
guidance comments (pre-authorized, since the MR itself is
permission-gated), runs the soundings analysis **once across all
compare URLs together** (so compound risks across repositories are
detected), and offers to post the report back to the MR as a new
comment under your own identity.

This repository is public but *running* the skill requires Red Hat
access: nothing in here is secret — the host name, project path, and
conventions all appear in the public
[RCS](https://github.com/RedHatInsights/release-confidence-score) repo.

## Requirements

- The [soundings](https://github.com/gwenneg/soundings) plugin (and its
  Go toolchain requirement — this helper is invoked via `go run` too)
- `GITLAB_TOKEN` set to your personal access token (api scope) for the
  app-interface GitLab host — also what soundings uses to fetch the
  compare URLs from that host
- VPN connectivity to that host

TLS is always verified — there is no skip option. The helper trusts the
operating system's certificate store, so a corporate CA installed on
your machine is used automatically.

## Configuration

| Env var | Meaning | Default |
|---|---|---|
| `GITLAB_TOKEN` | your personal access token (api scope) — required | none |
| `APP_INTERFACE_HOST` | app-interface GitLab host | `gitlab.cee.redhat.com` |
| `SOUNDINGS_FEEDBACK_URL` | compliance-mandated feedback link, inserted into the report by `annotate` | none |
| `SOUNDINGS_AUTO_DEPLOY_THRESHOLD` | score at/above which release is recommended | soundings default (80) |
| `SOUNDINGS_REVIEW_REQUIRED_THRESHOLD` | score at/above which review (instead of no-go) is recommended | soundings default (60) |

Authentication is your own personal token, and reports are posted under
its identity — there is no shared service account.

## How it relates to soundings

Everything Red Hat-specific lives here: the MR→compare-URL resolution,
the pre-authorized guidance convention, the thresholds plumbing, the
override-justification report banner and the compliance-mandated
feedback link (both inserted by this helper's own `annotate`
subcommand), and MR posting. The analysis itself — fetching, the
isolated assessment, scoring, rendering — is entirely soundings, invoked
by name with a documented parameter contract. Soundings never knows the
inputs came from app-interface.

Re-running on the same MR after new commits is safe and intended: the
resolver always reads the MR's current state, and each run posts a new
comment, keeping an audit trail of how the score evolved.

## Provenance

This is the skills-based successor to the *app-interface mode* of
[Release Confidence Score](https://github.com/RedHatInsights/release-confidence-score)
(RCS), originally built at Red Hat. The standalone analysis mode became
[soundings](https://github.com/gwenneg/soundings).

## License

[Apache-2.0](LICENSE)
