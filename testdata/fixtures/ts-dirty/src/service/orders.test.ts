// A test file: its imports are attributed as test-only edges.
import assert from "node:assert";

import { placeOrder } from "./orders";

const order = placeOrder(10);
assert.strictEqual(order.total, 10);
