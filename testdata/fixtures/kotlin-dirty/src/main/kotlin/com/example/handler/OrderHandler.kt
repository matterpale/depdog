// Handler layer: allowed service, std and external — but here it also reaches
// PAST the service layer straight into the domain, which the rules forbid.
package com.example.handler

import java.io.IOException

import com.squareup.moshi.Moshi

import com.example.domain.Order
import com.example.service.OrderService

class OrderHandler {
    private val service = OrderService()
    private val moshi = Moshi.Builder().build()

    @Throws(IOException::class)
    fun handle(total: Int): String {
        val order = service.placeOrder(total)
        val fresh = Order(order.total)
        return moshi.adapter(Long::class.java).toJson(fresh.id)
    }
}
