# A test file: its imports are attributed as test-only edges.
import unittest

from service.orders import place_order


class PlaceOrderTest(unittest.TestCase):
    def test_total(self):
        self.assertEqual(place_order(10).total, 10)
