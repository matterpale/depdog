// Domain layer: pure business types. Only the platform stdlib is allowed here.
package com.example.domain

import java.util.concurrent.atomic.AtomicLong

final class Order(val total: Int):
  val id: Long = Order.NextId.getAndIncrement()

  override def toString: String = s"order $id ($total)"

object Order:
  private val NextId = AtomicLong(1)
