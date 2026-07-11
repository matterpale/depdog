// Handler layer: the edge. May import service, stdlib and external deps.
package com.example.handler

import java.io.IOException

import io.circe.Json

import com.example.service.OrderService

final class OrderHandler:
  private val service = OrderService()

  @throws[IOException]
  def handle(total: Int): String =
    val order = service.placeOrder(total)
    Json.fromLong(order.id).noSpaces
