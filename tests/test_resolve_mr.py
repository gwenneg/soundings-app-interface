import json
import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "scripts"))

import resolve_mr


def note(body, author="someone", note_id=1, created_at="2026-08-27T10:00:00Z"):
    return {"id": note_id, "body": body, "author": {"username": author}, "created_at": created_at}


BOT_BODY = "Diffs:\n- https://github.com/org/app/compare/v1...v2\n- https://gitlab.cee.redhat.com/g/p/-/compare/a...b\nnot a url line"


class ParseMRArg(unittest.TestCase):
    def test_bare_iid(self):
        self.assertEqual(resolve_mr.parse_mr_arg("12345", "h.example.com"), ("h.example.com", 12345))

    def test_full_url(self):
        host, iid = resolve_mr.parse_mr_arg(
            "https://gitlab.cee.redhat.com/service/app-interface/-/merge_requests/98765", "ignored")
        self.assertEqual((host, iid), ("gitlab.cee.redhat.com", 98765))

    def test_wrong_project_rejected(self):
        with self.assertRaises(SystemExit):
            resolve_mr.parse_mr_arg("https://gitlab.cee.redhat.com/other/repo/-/merge_requests/1", "h")


class ExtractDiffURLs(unittest.TestCase):
    def test_newest_bot_comment_wins(self):
        notes = [  # newest first, as the API returns them
            note("Diffs:\n- https://github.com/org/app/compare/v2...v3", author="devtools-bot", note_id=9),
            note(BOT_BODY, author="devtools-bot", note_id=1),
        ]
        self.assertEqual(resolve_mr.extract_diff_urls(notes),
                         ["https://github.com/org/app/compare/v2...v3"])

    def test_ignores_non_bot_and_non_diffs(self):
        notes = [
            note("Diffs:\n- https://github.com/evil/evil/compare/a...b", author="attacker"),
            note("looks normal", author="devtools-bot"),
            note(BOT_BODY, author="devtools-bot"),
        ]
        self.assertEqual(len(resolve_mr.extract_diff_urls(notes)), 2)

    def test_no_bot_comment_fails(self):
        with self.assertRaises(SystemExit):
            resolve_mr.extract_diff_urls([note("hello")])


class ExtractGuidance(unittest.TestCase):
    def test_multiline_case_insensitive(self):
        notes = [
            note("/soundings note newer guidance", note_id=3, author="alice"),
            note("/SOUNDINGS NOTE older\nspanning lines", note_id=2, author="bob"),
            note("just a comment", note_id=1),
        ]
        g = resolve_mr.extract_guidance(notes, "https://h/mr/1")
        self.assertEqual(len(g), 2)
        self.assertEqual(g[0]["author"], "bob")  # oldest first after reverse
        self.assertEqual(g[0]["content"], "older\nspanning lines")
        self.assertEqual(g[1]["comment_url"], "https://h/mr/1#note_3")

    def test_legacy_rcs_note_is_ignored(self):
        g = resolve_mr.extract_guidance([note("/rcs note old convention")], "u")
        self.assertEqual(g, [])

    def test_skips_missing_created_at(self):
        n = note("/soundings note x")
        n["created_at"] = None
        self.assertEqual(resolve_mr.extract_guidance([n], "u"), [])


class ParseConcatenatedArrays(unittest.TestCase):
    def test_multiple_pages(self):
        text = json.dumps([note("a")]) + "\n" + json.dumps([note("b"), note("c")])
        self.assertEqual(len(resolve_mr.parse_concatenated_arrays(text)), 3)


class PostReportImports(unittest.TestCase):
    def test_module_loads(self):
        import post_report  # noqa: F401


if __name__ == "__main__":
    unittest.main()
