// API layer: allowed service, std and external — but here it also reaches PAST
// the service layer straight into the domain, which the rules forbid. This is
// the ONE deliberate violation in the monorepo fixture.
import express from "express";

import { placeOrder } from "@web/service/orders";
import { newOrder } from "../domain/order";

const app = express();

app.post("/orders", (req, res) => {
  const order = placeOrder(Number(req.body.total));
  res.json(newOrder(order.total));
});

export default app;
