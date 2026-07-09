# Domain layer: pure business types. Only the platform stdlib is allowed here.
import uuid
from dataclasses import dataclass


@dataclass
class Order:
    id: str
    total: int


def new_order(total: int) -> Order:
    return Order(id=str(uuid.uuid4()), total=total)
