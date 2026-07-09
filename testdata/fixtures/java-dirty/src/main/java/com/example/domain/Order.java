// Domain layer: should stay pure (std only) — but here it reaches UP into the
// service.notify layer, a layering inversion the rules forbid.
package com.example.domain;

import java.util.concurrent.atomic.AtomicLong;

import com.example.service.notify.Notifier;

public final class Order {
    private static final AtomicLong NEXT_ID = new AtomicLong(1);

    public final long id;
    public final int total;

    public Order(int total) {
        this.id = NEXT_ID.getAndIncrement();
        this.total = total;
        Notifier.notify(this.id);
    }

    @Override
    public String toString() {
        return "order " + id + " (" + total + ")";
    }
}
