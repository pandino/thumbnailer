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

    def test_derive_labels(self):
        labels = taste.derive_labels([
            d(1, 1, "slideshow", "delete"),
            d(2, 2, "slideshow", "pass"),
            d(3, 3, "slideshow", "archive"),
            d(4, 4, "player", "archive"),   # never judged in the slideshow: implied keep
            d(5, 5, "slideshow", "pass"),
            d(6, 5, "player", "delete"),
            d(7, 6, "slideshow", "delete"),
            d(8, 6, "slideshow", "undo"),   # undone, nothing else: no label
        ])
        self.assertEqual(labels["genre"], {1: 1, 2: 0, 3: 0, 4: 0, 5: 0})
        self.assertEqual(labels["watch"], {4: 1, 5: 0})

    def test_class_counts(self):
        self.assertEqual(taste.class_counts({1: 1, 2: 0, 3: 0}), (2, 1))


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
