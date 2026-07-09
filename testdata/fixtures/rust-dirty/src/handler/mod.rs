// Handler layer: allowed service, std and external — but here it also reaches
// PAST the service layer straight into the domain, which the rules forbid.
use std::io;

use serde::Serialize;

use crate::domain::new_order;
use crate::service::place_order;

#[derive(Serialize)]
struct Receipt {
    id: u64,
    total: u32,
}

pub fn handle(total: u32) -> io::Result<String> {
    let order = place_order(total);
    let fresh = new_order(order.total);
    let receipt = Receipt {
        id: fresh.id,
        total: fresh.total,
    };
    Ok(receipt.id.to_string())
}
