// A small service helper the domain wrongly reaches into.
package com.example.service.notify

object Notifier {
    fun notify(orderId: Long) {
        // no-op
        require(orderId >= 0)
    }
}
