# Service layer: orchestrates the domain. May import domain + stdlib.
import logging

from domain.order import Order, new_order

log = logging.getLogger(__name__)


def place_order(total: int) -> Order:
    order = new_order(total)
    log.info("placed order %s", order.id)
    return order
