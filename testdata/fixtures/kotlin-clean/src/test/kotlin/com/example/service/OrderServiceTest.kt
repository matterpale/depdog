// A test source: its imports are attributed as test-only edges (src/test tree).
package com.example.service

import com.example.domain.Order

class OrderServiceTest {
    fun totalIsPreserved(): Boolean {
        val order: Order = OrderService().placeOrder(10)
        return order.total == 10
    }
}
