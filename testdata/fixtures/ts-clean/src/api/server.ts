// API layer: the HTTP edge. May import service, stdlib and external deps.
import express from "express";

import { placeOrder } from "@app/service/orders";

const app = express();

app.post("/orders", (req, res) => {
  const order = placeOrder(Number(req.body.total));
  res.json(order);
});

export default app;
