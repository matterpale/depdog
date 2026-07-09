// Service layer: orchestrates the domain. May import domain + stdlib.
import { EventEmitter } from "events";

import { newOrder, type Order } from "../domain/order";

const bus = new EventEmitter();

export function placeOrder(total: number): Order {
  const order = newOrder(total);
  bus.emit("placed", order);
  return order;
}
