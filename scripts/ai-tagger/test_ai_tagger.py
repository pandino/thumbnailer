"""Unit tests for ai_tagger.py: python3 -m unittest discover -s scripts/ai-tagger"""

import os
import tempfile
import unittest

import ai_tagger


class CleanTagsTest(unittest.TestCase):
    def test_clean_tag(self):
        self.assertEqual(ai_tagger.clean_tag("  Big   Bunny "), "big bunny")
        for bad in ["", "   ", "a,b", "a/b", "x" * 65, None, 3]:
            self.assertIsNone(ai_tagger.clean_tag(bad), bad)
        self.assertEqual(ai_tagger.clean_tag("x" * 64), "x" * 64)

    def test_clean_tags_dedup_preserves_order(self):
        self.assertEqual(ai_tagger.clean_tags(["B", "a", "b ", "", "c/d"]), ["b", "a"])


class KeywordsTest(unittest.TestCase):
    def test_merge_cli_and_file(self):
        with tempfile.NamedTemporaryFile("w", suffix=".txt", delete=False, encoding="utf-8") as f:
            f.write("# comment line\nBeach\n\n  night sky  # trailing comment\nbeach\n")
            path = f.name
        try:
            self.assertEqual(ai_tagger.load_keywords("Interview, beach", path), ["interview", "beach", "night sky"])
        finally:
            os.unlink(path)

    def test_no_keywords(self):
        self.assertEqual(ai_tagger.load_keywords(None, None), [])


class SelectTagsTest(unittest.TestCase):
    def test_free_mode_caps_and_drops_marker(self):
        tags = ai_tagger.select_tags(["Cats", "ai-reviewed", "dogs", "cats", "birds"], ["cats"], False, 2, "ai-reviewed")
        self.assertEqual(tags, ["cats", "dogs"])

    def test_keywords_only(self):
        tags = ai_tagger.select_tags(["Cats", "dogs", "Beach"], ["beach", "cats"], True, 8, "ai-reviewed")
        self.assertEqual(tags, ["cats", "beach"])


class PromptTest(unittest.TestCase):
    def test_keywords_listed_before_existing_tags(self):
        prompt = ai_tagger.build_prompt(["beach", "night"], ["cats", "beach"], False, 5)
        self.assertIn("Preferred tags, use them whenever they fit: beach, night.", prompt)
        self.assertIn("Tags already in use, reuse them instead of synonyms: cats.", prompt)
        self.assertLess(prompt.index("beach, night"), prompt.index("cats"))
        self.assertIn("at most 5", prompt)

    def test_keywords_only_hides_existing_tags(self):
        prompt = ai_tagger.build_prompt(["beach"], ["cats"], True, 8)
        self.assertNotIn("cats", prompt)
        self.assertIn("Only use tags from the preferred list", prompt)

    def test_no_vocabulary(self):
        prompt = ai_tagger.build_prompt([], [], False, 8)
        self.assertNotIn("Preferred", prompt)
        self.assertNotIn("already in use", prompt)


class ParseAnswerTest(unittest.TestCase):
    def test_plain_and_fenced(self):
        self.assertEqual(ai_tagger.parse_model_answer('{"tags": ["a", "b"]}'), ["a", "b"])
        self.assertEqual(ai_tagger.parse_model_answer('```json\n{"tags": ["a"]}\n```'), ["a"])

    def test_invalid(self):
        for bad in ["no json here", '{"labels": []}', '{"tags": "a"}', None, "{broken"]:
            with self.assertRaises(ValueError, msg=bad):
                ai_tagger.parse_model_answer(bad)


class ArgsTest(unittest.TestCase):
    def test_keywords_only_requires_keywords(self):
        with self.assertRaises(SystemExit):
            ai_tagger.parse_args(["--server", "http://x", "--keywords-only"])

    def test_server_is_required(self):
        with self.assertRaises(SystemExit):
            ai_tagger.parse_args([])

    def test_router_defaults(self):
        args = ai_tagger.parse_args(["--server", "http://x"])
        self.assertEqual(args.api_url, "http://localhost:8080/v1")
        self.assertEqual(args.model, "gemma-4-12b-vision")

    def test_marker_is_normalized(self):
        self.assertEqual(ai_tagger.parse_args(["--server", "http://x", "--marker", " AI Reviewed "]).marker, "ai reviewed")


if __name__ == "__main__":
    unittest.main()
