// Handler layer: the edge. May import service, stdlib and external deps.
package com.example.handler

import java.io.IOException

import com.squareup.moshi.Moshi

import com.example.service.OrderService

class OrderHandler {
    private val service = OrderService()
    private val moshi = Moshi.Builder().build()

    @Throws(IOException::class)
    fun handle(total: Int): String {
        val order = service.placeOrder(total)
        return moshi.adapter(Long::class.java).toJson(order.id)
    }
}
