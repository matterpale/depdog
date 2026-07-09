// A test source: its imports are attributed as test-only edges (src/test tree).
package com.example.service;

import com.example.domain.Order;

public final class OrderServiceTest {
    public boolean totalIsPreserved() {
        Order order = new OrderService().placeOrder(10);
        return order.total == 10;
    }
}
