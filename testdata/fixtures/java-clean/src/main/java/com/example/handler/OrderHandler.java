// Handler layer: the edge. May import service, stdlib and external deps.
package com.example.handler;

import java.io.IOException;

import com.google.gson.Gson;

import com.example.service.OrderService;

public final class OrderHandler {
    private final OrderService service = new OrderService();
    private final Gson gson = new Gson();

    public String handle(int total) throws IOException {
        return gson.toJson(service.placeOrder(total).id);
    }
}
