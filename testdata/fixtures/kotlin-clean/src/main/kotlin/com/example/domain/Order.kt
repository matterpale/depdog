// Domain layer: pure business types. Only the platform stdlib is allowed here.
package com.example.domain

import java.util.concurrent.atomic.AtomicLong

class Order(val total: Int) {
    val id: Long = NEXT_ID.getAndIncrement()

    override fun toString(): String = "order $id ($total)"

    companion object {
        private val NEXT_ID = AtomicLong(1)
    }
}
