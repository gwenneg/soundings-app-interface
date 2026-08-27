#!/usr/bin/env python3
"""Post a soundings report as a NEW comment on an app-interface MR.

Always a new comment, never an edit - re-runs leave a visible audit trail
of how the score evolved as the MR changed. Posts under the user's own
glab identity.

Usage: post_report.py <MR IID or MR URL> <report markdown file>
Prints the URL of the posted comment.
"""

import json
import os
import subprocess
import sys

from resolve_mr import DEFAULT_HOST, PROJECT, fail, parse_mr_arg


def main():
    if len(sys.argv) != 3:
        fail("usage: post_report.py <MR IID or MR URL> <report markdown file>")
    host = os.environ.get("APP_INTERFACE_HOST", DEFAULT_HOST)
    host, iid = parse_mr_arg(sys.argv[1], host)
    try:
        with open(sys.argv[2], encoding="utf-8") as f:
            body = f.read()
    except OSError as e:
        fail(f"cannot read report file: {e}")
    if not body.strip():
        fail("report file is empty - refusing to post an empty comment")

    path = f"projects/{PROJECT.replace('/', '%2F')}/merge_requests/{iid}/notes"
    proc = subprocess.run(
        ["glab", "api", "--hostname", host, "-X", "POST", path, "-f", f"body={body}"],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        fail(f"posting comment failed: {proc.stderr.strip()}")
    note = json.loads(proc.stdout)
    print(f"https://{host}/{PROJECT}/-/merge_requests/{iid}#note_{note['id']}")


if __name__ == "__main__":
    main()
