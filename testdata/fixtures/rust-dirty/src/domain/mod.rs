// Domain layer: should stay pure (std only) — but here it reaches UP into the
// service layer, a layering inversion the rules forbid.
use std::fmt;
use std::sync::atomic::{AtomicU64, Ordering};

use crate::service::notify::notify;

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
    let order = Order {
        id: NEXT_ID.fetch_add(1, Ordering::Relaxed),
        total,
    };
    notify(order.id);
    order
}
