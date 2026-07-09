// Domain layer: should stay pure (std only) — but here it reaches UP into the
// service.notify layer, a layering inversion the rules forbid.
package com.example.domain

import java.util.concurrent.atomic.AtomicLong

import com.example.service.notify.Notifier

class Order(val total: Int) {
    val id: Long = NEXT_ID.getAndIncrement()

    init {
        Notifier.notify(id)
    }

    override fun toString(): String = "order $id ($total)"

    companion object {
        private val NEXT_ID = AtomicLong(1)
    }
}
