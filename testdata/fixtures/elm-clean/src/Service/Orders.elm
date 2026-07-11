module Service.Orders exposing (place)

{- Service layer: orchestrates the domain. May import domain + stdlib. -}

import Dict exposing (Dict)
import Domain.Order as Order exposing (Order)


place : List Int -> Order
place lines =
    { lines = lines }


index : List Order -> Dict Int Int
index orders =
    Dict.fromList (List.indexedMap (\i o -> ( i, Order.subtotal o )) orders)
