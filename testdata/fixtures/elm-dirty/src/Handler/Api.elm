module Handler.Api exposing (view)

-- Handler layer: the edge. May import service, stdlib and external deps
-- (Html is elm/html, Json.Decode is elm/json — both external, not std).

import Html exposing (Html, text)
import Json.Decode as Decode
import Service.Orders as Orders


view : List Int -> Html msg
view lines =
    let
        order =
            Orders.place lines
    in
    text (String.fromInt (List.sum order.lines))


decoder : Decode.Decoder (List Int)
decoder =
    Decode.list Decode.int
