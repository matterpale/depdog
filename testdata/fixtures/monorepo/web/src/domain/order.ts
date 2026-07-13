// Domain layer: pure business types. Only the platform stdlib is allowed here.
import { randomUUID } from "node:crypto";

export interface Order {
  id: string;
  total: number;
}

export function newOrder(total: number): Order {
  return { id: randomUUID(), total };
}
