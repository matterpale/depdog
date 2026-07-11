// Handler layer: allowed service, std and external — but here it also reaches
// PAST the service layer straight into the domain, which the rules forbid.
package com.example.handler

import java.io.IOException

import io.circe.Json

import com.example.domain.Order
import com.example.service.OrderService

final class OrderHandler:
  private val service = OrderService()

  @throws[IOException]
  def handle(total: Int): String =
    val order = service.placeOrder(total)
    val fresh = Order(order.total)
    Json.fromLong(fresh.id).noSpaces
