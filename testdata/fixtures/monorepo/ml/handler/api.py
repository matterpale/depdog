# Handler layer: the HTTP edge. May import service, stdlib and external deps.
import json

import requests

from service.orders import place_order


def handle(total: int) -> str:
    order = place_order(total)
    requests.post("https://example.test/orders", json={"id": order.id})
    return json.dumps({"id": order.id, "total": order.total})
