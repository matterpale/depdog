module Service.Orders exposing (place)

{- Service layer: orchestrates the domain. May import domain + stdlib. -}

import Dict exposing (Dict)


place : List Int -> { lines : List Int }
place lines =
    { lines = lines }


counts : List Int -> Dict Int Int
counts lines =
    Dict.fromList (List.indexedMap Tuple.pair lines)
