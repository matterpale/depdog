module Domain.Order exposing (Order, subtotal)

-- Domain layer: pure business types. Only the elm/core stdlib is allowed here.

import List


type alias Order =
    { lines : List Int }


subtotal : Order -> Int
subtotal order =
    List.sum order.lines
