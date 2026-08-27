#!/usr/bin/env python3
"""Resolve an app-interface MR into soundings inputs.

Fetches the MR's notes via `glab api` (the user's own authenticated CLI),
finds the newest devtools-bot "Diffs:" comment, and extracts the compare
URLs plus all "/soundings note" guidance comments. app-interface guidance
is unconditionally authorized - the MR itself is permission-gated.

Output (stdout): one JSON object:
  {
    "mr_url": "...",
    "diff_urls": ["..."],
    "guidance": [{"content", "author", "date", "comment_url"}, ...],
    "feedback_url": "...",        # only when APP_INTERFACE_FEEDBACK_URL is set
    "auto_deploy": 80,            # only when APP_INTERFACE_AUTO_DEPLOY_THRESHOLD is set
    "review_required": 60         # only when APP_INTERFACE_REVIEW_REQUIRED_THRESHOLD is set
  }

The script only reads MR notes; it never touches the diffs themselves -
fetching diff content stays inside the soundings helper.
"""

import json
import os
import re
import subprocess
import sys

PROJECT = "service/app-interface"
BOT_USERNAME = "devtools-bot"
DIFFS_MARKER = "Diffs:"
DEFAULT_HOST = "gitlab.cee.redhat.com"

# URLs on lines starting with "- " inside the bot comment.
URL_RE = re.compile(r"^- (https?://\S+)$", re.MULTILINE)
# "/soundings note <text>", multiline and case-insensitive.
GUIDANCE_RE = re.compile(r"^\s*/soundings\s+note\s+(\S.*\S|\S)", re.IGNORECASE | re.DOTALL)

MR_URL_RE = re.compile(r"^https?://([^/]+)/(.+)/-/merge_requests/(\d+)")


def parse_mr_arg(arg, default_host):
    """Accept a bare IID or a full MR URL; return (host, iid)."""
    m = MR_URL_RE.match(arg)
    if m:
        host, project, iid = m.group(1), m.group(2), int(m.group(3))
        if project != PROJECT:
            fail(f"expected an MR of {PROJECT}, got {project}")
        return host, iid
    if arg.isdigit():
        return default_host, int(arg)
    fail(f"expected an MR IID or MR URL, got {arg!r}")


def fetch_notes(host, iid):
    """All MR notes, newest first, via glab api with pagination."""
    path = (
        f"projects/{PROJECT.replace('/', '%2F')}/merge_requests/{iid}"
        f"/notes?per_page=100&order_by=created_at&sort=desc"
    )
    try:
        proc = subprocess.run(
            ["glab", "api", "--hostname", host, "--paginate", path],
            capture_output=True, text=True,
        )
    except FileNotFoundError:
        fail("glab is not installed - install it and run 'glab auth login --hostname " + host + "'")
    if proc.returncode != 0:
        err = proc.stderr.strip()
        if "no such host" in err or "dial tcp" in err or "i/o timeout" in err:
            fail(f"cannot reach {host} - are you on the VPN? ({err})")
        if "401" in err or "403" in err or "authenticate" in err.lower():
            fail(f"authentication to {host} failed - run 'glab auth login --hostname {host}' ({err})")
        fail(f"glab api failed: {err}")
    return parse_concatenated_arrays(proc.stdout)


def parse_concatenated_arrays(text):
    """`glab api --paginate` emits one JSON array per page, concatenated."""
    notes, decoder, i = [], json.JSONDecoder(), 0
    text = text.strip()
    while i < len(text):
        page, end = decoder.raw_decode(text, i)
        notes.extend(page)
        i = end
        while i < len(text) and text[i] in " \n\r\t":
            i += 1
    return notes


def extract_diff_urls(notes):
    """URLs from the newest devtools-bot Diffs comment (notes arrive newest first)."""
    for note in notes:
        author = (note.get("author") or {}).get("username")
        body = note.get("body") or ""
        if author != BOT_USERNAME or not body.startswith(DIFFS_MARKER):
            continue
        urls = URL_RE.findall(body)
        if urls:
            return urls
    fail(
        f"no '{DIFFS_MARKER}' comment from {BOT_USERNAME} found on this MR - "
        "either it is not a deployment MR, or the bot has not commented yet"
    )


def extract_guidance(notes, mr_url):
    guidance = []
    for note in notes:
        m = GUIDANCE_RE.match(note.get("body") or "")
        if not m or not note.get("created_at"):
            continue
        guidance.append({
            "content": m.group(1),
            "author": (note.get("author") or {}).get("username", ""),
            "date": note["created_at"],
            "comment_url": f"{mr_url}#note_{note['id']}",
        })
    guidance.reverse()  # notes arrive newest first; report reads best oldest first
    return guidance


def fail(msg):
    print(f"resolve_mr: {msg}", file=sys.stderr)
    sys.exit(1)


def main():
    if len(sys.argv) != 2:
        fail("usage: resolve_mr.py <MR IID or MR URL>")
    host = os.environ.get("APP_INTERFACE_HOST", DEFAULT_HOST)
    host, iid = parse_mr_arg(sys.argv[1], host)
    mr_url = f"https://{host}/{PROJECT}/-/merge_requests/{iid}"

    notes = fetch_notes(host, iid)
    out = {
        "mr_url": mr_url,
        "diff_urls": extract_diff_urls(notes),
        "guidance": extract_guidance(notes, mr_url),
    }
    if os.environ.get("APP_INTERFACE_FEEDBACK_URL"):
        out["feedback_url"] = os.environ["APP_INTERFACE_FEEDBACK_URL"]
    for env, key in [
        ("APP_INTERFACE_AUTO_DEPLOY_THRESHOLD", "auto_deploy"),
        ("APP_INTERFACE_REVIEW_REQUIRED_THRESHOLD", "review_required"),
    ]:
        if os.environ.get(env):
            out[key] = int(os.environ[env])
    json.dump(out, sys.stdout, indent=2)
    print()


if __name__ == "__main__":
    main()
