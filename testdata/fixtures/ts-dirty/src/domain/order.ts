// Domain layer: should stay pure (std only) — but here it reaches UP into the
// service layer, a layering inversion the rules forbid.
import { randomUUID } from "node:crypto";

import { notify } from "../service/notify";

export interface Order {
  id: string;
  total: number;
}

export function newOrder(total: number): Order {
  const order = { id: randomUUID(), total };
  notify(order.id);
  return order;
}
