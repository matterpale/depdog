// Service layer: orchestrates the domain. May import domain + stdlib.
package com.example.service

import scala.collection.mutable.{Map => MutableMap}

import com.example.domain.Order

final class OrderService:
  def placeOrder(total: Int): Order =
    val log = MutableMap.empty[Long, Int]
    val order = Order(total)
    log(order.id) = order.total
    order
