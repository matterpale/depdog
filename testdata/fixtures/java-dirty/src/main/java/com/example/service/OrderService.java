// Service layer: orchestrates the domain. May import domain + stdlib.
package com.example.service;

import java.util.HashMap;
import java.util.Map;

import com.example.domain.Order;

public final class OrderService {
    public Order placeOrder(int total) {
        Map<Long, Integer> log = new HashMap<>();
        Order order = new Order(total);
        log.put(order.id, order.total);
        return order;
    }
}
