// Service layer: orchestrates the domain. May import domain + stdlib.
package com.example.service

import kotlin.collections.mutableMapOf

import com.example.domain.Order

class OrderService {
    fun placeOrder(total: Int): Order {
        val log = mutableMapOf<Long, Int>()
        val order = Order(total)
        log[order.id] = order.total
        return order
    }
}
