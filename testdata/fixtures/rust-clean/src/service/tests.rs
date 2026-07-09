// A test module: its imports are attributed as test-only edges.
use crate::service::place_order;

#[test]
fn total_is_preserved() {
    assert_eq!(place_order(10).total, 10);
}
