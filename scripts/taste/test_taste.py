import unittest

import taste


def d(id, tid, stage, action):
    return {"id": id, "thumbnail_id": tid, "stage": stage, "action": action}


class TileBoxesTest(unittest.TestCase):
    GEOMETRY = {"cols": 8, "rows": 4, "tile_width": 320, "tile_height": 180, "padding": 4}

    def test_standard_grid(self):
        boxes = taste.tile_boxes(self.GEOMETRY)
        self.assertEqual(len(boxes), 32)
        self.assertEqual(boxes[0], (4, 4, 324, 184))
        self.assertEqual(boxes[1], (328, 4, 648, 184))
        self.assertEqual(boxes[8], (4, 188, 324, 368))
        # The last tile ends one margin before the edge of the 2596x740 image
        self.assertEqual(boxes[-1], (2272, 556, 2592, 736))

    def test_unknown_layout(self):
        self.assertEqual(taste.tile_boxes({"cols": 0, "rows": 0}), [])
        self.assertEqual(taste.tile_boxes({}), [])


class UnknownLayoutTest(unittest.TestCase):
    ITEMS = [
        {"id": 1, "geometry": {"cols": 8, "rows": 4, "tile_width": 320, "tile_height": 180, "padding": 4}},
        {"id": 2, "geometry": {"cols": 0, "rows": 0, "tile_width": 0, "tile_height": 0, "padding": 0}},
    ]

    def test_unknown_skipped_by_default(self):
        keep, skipped = taste.split_unknown_layout(self.ITEMS, False)
        self.assertEqual([i["id"] for i in keep], [1])
        self.assertEqual(skipped, 1)

    def test_unknown_included_on_request(self):
        keep, skipped = taste.split_unknown_layout(self.ITEMS, True)
        self.assertEqual([i["id"] for i in keep], [1, 2])
        self.assertEqual(skipped, 0)


class SpreadTest(unittest.TestCase):
    def test_evenly_spaced(self):
        self.assertEqual(taste.spread(list(range(32)), 4), [0, 8, 16, 24])
        self.assertEqual(taste.spread(list(range(10)), 3), [0, 3, 6])

    def test_keeps_all_when_few_or_unlimited(self):
        self.assertEqual(taste.spread([1, 2], 16), [1, 2])
        self.assertEqual(taste.spread([1, 2, 3], 0), [1, 2, 3])


class LabelsTest(unittest.TestCase):
    def test_final_actions_apply_undo(self):
        finals = taste.final_actions([
            d(1, 10, "slideshow", "delete"),
            d(2, 10, "slideshow", "undo"),
            d(3, 10, "slideshow", "pass"),
            d(4, 11, "slideshow", "delete"),
            d(5, 11, "slideshow", "undo"),
            d(6, 12, "slideshow", "undo"),  # undo of nothing is ignored
        ])
        self.assertEqual(finals, {(10, "slideshow"): "pass"})

    def test_log_order_not_list_order(self):
        finals = taste.final_actions([d(2, 10, "slideshow", "pass"), d(1, 10, "slideshow", "delete")])
        self.assertEqual(finals[(10, "slideshow")], "pass")



class OutcomesTest(unittest.TestCase):
    def test_final_outcome_wherever_decided(self):
        result = taste.outcomes([
            d(1, 1, "slideshow", "delete"),
            d(2, 2, "slideshow", "pass"),       # undecided: wants to watch it first
            d(3, 3, "slideshow", "archive"),     # archived without watching
            d(4, 4, "player", "archive"),
            d(5, 5, "slideshow", "pass"),
            d(6, 5, "player", "delete"),         # passed, then deleted after watching
            d(7, 6, "slideshow", "delete"),
            d(8, 6, "slideshow", "undo"),        # undone, nothing else: no outcome
            d(9, 7, "slideshow", "archive"),
            d(10, 7, "player", "delete"),        # the later of two final decisions wins
        ])
        D, U, A = taste.DELETED, taste.UNDECIDED, taste.ARCHIVED
        self.assertEqual(result, {1: D, 2: U, 3: A, 4: A, 5: D, 7: D})

    def test_final_decisions_record_the_stage(self):
        D, U, A = taste.DELETED, taste.UNDECIDED, taste.ARCHIVED
        final = taste.final_decisions([
            d(1, 1, "slideshow", "delete"),
            d(2, 2, "slideshow", "pass"),
            d(3, 2, "player", "delete"),     # passed, deleted after watching
            d(4, 3, "slideshow", "pass"),
            d(5, 3, "player", "archive"),
            d(6, 4, "slideshow", "pass"),
        ])
        self.assertEqual(final, {1: (D, "slideshow"), 2: (D, "player"), 3: (A, "player"), 4: (U, "slideshow")})

    def test_watched_deletes_weigh_less(self):
        D, U, A = taste.DELETED, taste.UNDECIDED, taste.ARCHIVED
        final = {1: (D, "slideshow"), 2: (D, "player"), 3: (A, "player"), 4: (U, "slideshow")}
        self.assertEqual(taste.delete_weights(final, [1, 2, 3, 4], 0.5), [1.0, 0.5, 1.0, 1.0])

    def test_watched_delete_weight_option(self):
        self.assertEqual(taste.parse_args(["--server", "http://x"]).watched_delete_weight, 0.5)
        with self.assertRaises(SystemExit):
            taste.parse_args(["--server", "http://x", "--watched-delete-weight", "0"])

    def test_outcome_summary(self):
        D, U, A = taste.DELETED, taste.UNDECIDED, taste.ARCHIVED
        self.assertEqual(taste.outcome_summary([D, U, U, A]), "1 deleted / 2 undecided / 1 archived")

    def test_consistent_probabilities(self):
        self.assertEqual(taste.consistent(0.2, 0.3), (0.2, 0.3))
        p_delete, p_archive = taste.consistent(0.6, 0.6)
        self.assertAlmostEqual(p_delete + p_archive, 1.0)
        self.assertAlmostEqual(p_delete, 0.5)


@unittest.skipUnless(__import__("importlib").util.find_spec("sklearn"), "scikit-learn not installed")
class CalibrationTest(unittest.TestCase):
    def test_probabilities_match_observed_frequencies(self):
        import numpy as np

        # 10% positives, partly separable: balanced weights alone would put the average
        # predicted probability near 50%
        rng = np.random.default_rng(0)
        y = (rng.random(3000) < 0.1).astype(int)
        X = rng.normal(size=(3000, 8)) + y[:, None] * 0.8
        model, auc, brier = taste.train(X, y, "delete")
        p = model.predict(X)
        self.assertAlmostEqual(p.mean(), y.mean(), delta=0.02)
        self.assertGreater(auc, 0.8)
        # Sample weights are accepted, and down-weighting positives lowers their predictions
        w = np.where(y == 1, 0.3, 1.0)
        weighted, _, _ = taste.train(X, y, "delete", w)
        self.assertLess(weighted.predict(X).mean(), p.mean())
        # Calibration is monotonic: the ranking is the balanced model's ranking
        raw = model.model.decision_function(X)
        self.assertTrue(np.all(np.diff(p[np.argsort(raw)]) >= -1e-12))


class RunEveryTest(unittest.TestCase):
    class FakeWorker:
        def __init__(self, outcomes, stop_after):
            self.outcomes = list(outcomes)
            self.stop_after = stop_after
            self.runs = 0
            self.stop = False

        def run(self):
            self.runs += 1
            outcome = self.outcomes.pop(0) if self.outcomes else None
            if isinstance(outcome, Exception):
                raise outcome
            return 0

    def test_option(self):
        self.assertEqual(taste.parse_args(["--server", "http://x"]).every, 0)
        self.assertEqual(taste.parse_args(["--server", "http://x", "--every", "15"]).every, 15)
        with self.assertRaises(SystemExit):
            taste.parse_args(["--server", "http://x", "--every", "-1"])

    def test_keeps_running_after_failures_until_stopped(self):
        worker = self.FakeWorker([taste.urllib.error.URLError("down"), RuntimeError("bad"), None], 3)
        waits = []

        def wait(w, seconds):
            waits.append(seconds)
            if w.runs >= w.stop_after:
                w.stop = True

        with self.assertLogs("taste", level="ERROR"):
            self.assertEqual(taste.run_every(worker, 15, wait), 0)
        self.assertEqual(worker.runs, 3)
        self.assertEqual(waits, [900, 900, 900])

    def test_stop_during_run_skips_the_wait(self):
        worker = self.FakeWorker([], 1)
        worker.run = lambda: setattr(worker, "stop", True)
        waits = []
        taste.run_every(worker, 15, lambda w, s: waits.append(s))
        self.assertEqual(waits, [])

    def test_sleep_wakes_when_stopped(self):
        worker = self.FakeWorker([], 1)
        worker.stop = True
        start = taste.time.monotonic()
        taste.sleep_unless_stopped(worker, 60)
        self.assertLess(taste.time.monotonic() - start, 0.5)


class DeviceTest(unittest.TestCase):
    def test_resolve_device(self):
        self.assertEqual(taste.resolve_device("cpu", True), "cpu")
        self.assertEqual(taste.resolve_device("cuda", True), "cuda")
        self.assertEqual(taste.resolve_device("auto", True), "cuda")
        self.assertEqual(taste.resolve_device("auto", False), "cpu")

    def test_explicit_gpu_without_one_fails(self):
        with self.assertRaises(SystemExit):
            taste.resolve_device("cuda", False)

    def test_default_is_cpu(self):
        self.assertEqual(taste.parse_args(["--server", "http://x"]).device, "cpu")


class GridDownloadTest(unittest.TestCase):
    def setUp(self):
        self.server = taste.Server("http://thumbnailer", timeout=1)
        self.original = taste.http_request

    def tearDown(self):
        taste.http_request = self.original

    def fail_with(self, code):
        def request(method, url, body=None, timeout=60):
            raise taste.urllib.error.HTTPError(url, code, "error", {}, None)
        taste.http_request = request

    def test_missing_grid_is_none(self):
        self.fail_with(404)
        self.assertIsNone(self.server.grid("/thumbnails/gone.jpg"))

    def test_other_errors_still_raise(self):
        self.fail_with(500)
        with self.assertRaises(taste.urllib.error.HTTPError):
            self.server.grid("/thumbnails/a.jpg")

    def test_found_grid_is_returned(self):
        taste.http_request = lambda method, url, body=None, timeout=60: b"jpeg"
        self.assertEqual(self.server.grid("/thumbnails/a.jpg"), b"jpeg")


@unittest.skipUnless(__import__("importlib").util.find_spec("PIL"), "Pillow not installed")
class FramesTest(unittest.TestCase):
    def test_blank_tiles_dropped(self):
        from PIL import Image, ImageDraw

        geometry = {"cols": 2, "rows": 1, "tile_width": 20, "tile_height": 10, "padding": 2}
        image = Image.new("RGB", (46, 14))
        # Only the first tile has content; the second stays black
        ImageDraw.Draw(image).rectangle((2, 2, 11, 11), fill="white")
        frames = taste.frames_of(image, geometry)
        self.assertEqual(len(frames), 1)
        self.assertEqual(frames[0].size, (20, 10))

    def test_whole_image_when_nothing_usable(self):
        from PIL import Image

        image = Image.new("RGB", (46, 14))
        self.assertEqual(taste.frames_of(image, {"cols": 2, "rows": 1, "tile_width": 20,
                                                  "tile_height": 10, "padding": 2})[0].size, (46, 14))
        self.assertEqual(taste.frames_of(image, {"cols": 0})[0].size, (46, 14))


if __name__ == "__main__":
    unittest.main()
