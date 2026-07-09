// Domain layer: pure business types. Only the platform stdlib is allowed here.
use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};

static NEXT_ID: AtomicU64 = AtomicU64::new(1);

pub struct Order {
    pub id: u64,
    pub total: u32,
}

impl fmt::Display for Order {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "order {} ({})", self.id, self.total)
    }
}

pub fn new_order(total: u32) -> Order {
    Order {
        id: NEXT_ID.fetch_add(1, Ordering::Relaxed),
        total,
    }
}
