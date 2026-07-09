// Handler layer: allowed service, std and external — but here it also reaches
// PAST the service layer straight into the domain, which the rules forbid.
package com.example.handler;

import java.io.IOException;

import com.google.gson.Gson;

import com.example.domain.Order;
import com.example.service.OrderService;

public final class OrderHandler {
    private final OrderService service = new OrderService();
    private final Gson gson = new Gson();

    public String handle(int total) throws IOException {
        Order order = service.placeOrder(total);
        Order fresh = new Order(order.total);
        return gson.toJson(fresh.id);
    }
}
