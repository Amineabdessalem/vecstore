"""Runs against a real server: set VECSTORE_URL (and VECSTORE_API_KEY if needed)."""

import os
import unittest
import uuid

from vecstore_client import APIError, Client, NotFound

URL = os.environ.get("VECSTORE_URL")


@unittest.skipUnless(URL, "set VECSTORE_URL to run against a server")
class ClientTest(unittest.TestCase):
    def setUp(self):
        self.c = Client(URL, api_key=os.environ.get("VECSTORE_API_KEY"))
        self.name = "py-" + uuid.uuid4().hex[:8]
        self.c.create_collection(self.name, 2, "euclidean")
        self.addCleanup(self.c.drop_collection, self.name)

    def test_round_trip(self):
        n = self.c.upsert(self.name, [
            {"id": "a", "values": [0, 0], "metadata": {"lang": "en"}},
            {"id": "b", "values": [1, 1], "metadata": {"lang": "fr"}},
            {"id": "c/d", "values": [5, 5], "metadata": {"lang": "en"}},
        ])
        self.assertEqual(n, 3)
        hits = self.c.search(self.name, [0.9, 0.9], k=2)
        self.assertEqual(hits[0].id, "b")
        hits = self.c.search(self.name, [0.9, 0.9], k=1, filter={"lang": "en"})
        self.assertEqual([h.id for h in hits], ["a"])
        self.assertEqual(self.c.get(self.name, "c/d")["values"], [5, 5])
        self.c.delete(self.name, "c/d")
        with self.assertRaises(NotFound):
            self.c.get(self.name, "c/d")
        self.assertEqual(self.c.collection(self.name)["count"], 2)
        self.assertGreaterEqual(self.c.compact(self.name), 0)
        self.assertIn(self.name, [c["name"] for c in self.c.collections()])

    def test_errors(self):
        with self.assertRaises(APIError) as ctx:
            self.c.create_collection(self.name, 2)
        self.assertEqual(ctx.exception.status, 409)
        with self.assertRaises(APIError) as ctx:
            self.c.search(self.name, [1.0])
        self.assertEqual(ctx.exception.status, 400)


if __name__ == "__main__":
    unittest.main()
