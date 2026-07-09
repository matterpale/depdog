// Service layer: orchestrates the domain. May import domain + stdlib.
use std::collections::HashMap;

use crate::domain::{new_order, Order};

pub fn place_order(total: u32) -> Order {
    let mut log: HashMap<u64, u32> = HashMap::new();
    let order = new_order(total);
    log.insert(order.id, order.total);
    order
}
