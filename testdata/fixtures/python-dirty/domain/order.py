# Domain layer: should stay pure (std only) — but here it reaches UP into the
# service layer, a layering inversion the rules forbid.
import uuid
from dataclasses import dataclass

from service.notify import notify


@dataclass
class Order:
    id: str
    total: int


def new_order(total: int) -> Order:
    order = Order(id=str(uuid.uuid4()), total=total)
    notify(order.id)
    return order
