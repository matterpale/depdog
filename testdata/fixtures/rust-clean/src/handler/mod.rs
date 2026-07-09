// Handler layer: the edge. May import service, stdlib and external deps.
use std::io;

use serde::Serialize;

use crate::service::place_order;

#[derive(Serialize)]
struct Receipt {
    id: u64,
    total: u32,
}

pub fn handle(total: u32) -> io::Result<String> {
    let order = place_order(total);
    let receipt = Receipt {
        id: order.id,
        total: order.total,
    };
    Ok(receipt.id.to_string())
}
