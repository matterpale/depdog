# Handler layer: allowed service, std and external — but here it also reaches
# PAST the service layer straight into the domain, which the rules forbid.
import json

import requests

from domain.order import new_order
from service.orders import place_order


def handle(total: int) -> str:
    order = place_order(total)
    fresh = new_order(order.total)
    requests.post("https://example.test/orders", json={"id": fresh.id})
    return json.dumps({"id": fresh.id, "total": fresh.total})
