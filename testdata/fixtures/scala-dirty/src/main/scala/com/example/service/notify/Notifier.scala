// A small service helper the domain wrongly reaches into.
package com.example.service.notify

object Notifier:
  def notify(orderId: Long): Unit =
    // no-op
    require(orderId >= 0)
