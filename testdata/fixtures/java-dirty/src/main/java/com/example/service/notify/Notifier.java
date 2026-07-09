// A small service helper the domain wrongly reaches into.
package com.example.service.notify;

public final class Notifier {
    private Notifier() {
    }

    public static void notify(long orderId) {
        // no-op
        assert orderId >= 0;
    }
}
