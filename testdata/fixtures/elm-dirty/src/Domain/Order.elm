module Domain.Order exposing (Order, subtotal)

-- Domain layer: should stay pure (elm/core std only) — but here it reaches UP
-- into the Service layer, a layering inversion the rules forbid.

import List
import Service.Orders


type alias Order =
    { lines : List Int }


subtotal : Order -> Int
subtotal order =
    List.sum order.lines


rebuild : List Int -> Order
rebuild lines =
    Service.Orders.place lines
