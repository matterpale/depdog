// Domain layer: should stay pure (std only) — but here it reaches UP into the
// service.notify layer, a layering inversion the rules forbid.
package com.example.domain

import java.util.concurrent.atomic.AtomicLong

import com.example.service.notify.Notifier

final class Order(val total: Int):
  val id: Long = Order.NextId.getAndIncrement()
  Notifier.notify(id)

  override def toString: String = s"order $id ($total)"

object Order:
  private val NextId = AtomicLong(1)
