// Domain layer: pure business types. Only the platform stdlib is allowed here.
package com.example.domain;

import java.util.concurrent.atomic.AtomicLong;

public final class Order {
    private static final AtomicLong NEXT_ID = new AtomicLong(1);

    public final long id;
    public final int total;

    public Order(int total) {
        this.id = NEXT_ID.getAndIncrement();
        this.total = total;
    }

    @Override
    public String toString() {
        return "order " + id + " (" + total + ")";
    }
}
