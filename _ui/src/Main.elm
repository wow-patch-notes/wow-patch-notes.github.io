port module Main exposing (main)

import Browser
import Dict exposing (Dict)
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Http
import Json.Decode as Decode
import Process
import Set exposing (Set)
import Task


port filtersChanged : Args -> Cmd msg


main : Program Args Model Msg
main =
    Browser.element
        { init = init
        , subscriptions = subscriptions
        , update = update
        , view = view
        }


type alias Model =
    { changes : Changes
    , patch : String
    , pageSize : Int
    , tagSet : Set String
    , searchTerm : String
    , tagFilters : Dict String TagFilterState
    }


type Changes
    = Loading
    | Changes (List Change)
    | Error Http.Error


type alias Change =
    { date : String
    , weekday : String
    , tags : Set String
    , text : String
    , url : String
    }


type TagFilterState
    = Require
    | Exclude
    | Ignore


changeDecoder : Decode.Decoder Change
changeDecoder =
    Decode.map5 Change
        (Decode.field "Date" Decode.string)
        (Decode.field "Weekday" Decode.string)
        (Decode.field "Tags" (Decode.list Decode.string |> Decode.map Set.fromList))
        (Decode.field "Text" Decode.string)
        (Decode.field "URL" Decode.string)


type alias Args =
    { patch : String
    , searchTerm : String
    , includeTags : List String
    , excludeTags : List String
    }


init : Args -> ( Model, Cmd Msg )
init args =
    let
        tags getter value =
            getter args
                |> List.map (\t -> ( t, value ))
                |> Dict.fromList
    in
    ( { changes = Loading
      , patch = args.patch
      , pageSize = 5
      , tagSet = Set.empty
      , searchTerm = args.searchTerm
      , tagFilters =
            Dict.empty
                |> Dict.union (tags .excludeTags Exclude)
                |> Dict.union (tags .includeTags Require)
      }
    , loadChanges args.patch
    )


loadChanges : String -> Cmd Msg
loadChanges patch =
    Http.get
        { url = "/wow-" ++ patch ++ "-patch-notes.json"
        , expect =
            Http.expectJson GotChanges
                (Decode.at [ "Changes" ] <| Decode.list changeDecoder)
        }


view : Model -> Html Msg
view model =
    div [] <|
        case model.changes of
            Loading ->
                [ p [] [ text "Loading …" ] ]

            Changes [] ->
                [ div [] [ viewPatchSelect model ]
                , p [] [ em [] [ text "No changes have been published for the selected season." ] ]
                ]

            Changes changes ->
                viewPage model changes

            Error (Http.BadUrl err) ->
                [ div [] [ viewPatchSelect model ]
                , p [] [ text "Cannot load patch notes: ", text err ]
                ]

            Error Http.Timeout ->
                [ div [] [ viewPatchSelect model ]
                , p [] [ text "Cannot load patch notes: timeout" ]
                ]

            Error Http.NetworkError ->
                [ div [] [ viewPatchSelect model ]
                , p [] [ text "Cannot load patch notes: network error" ]
                ]

            Error (Http.BadStatus status) ->
                [ div [] [ viewPatchSelect model ]
                , p [] [ text "Cannot load patch notes: HTTP status ", text <| String.fromInt status ]
                ]

            Error (Http.BadBody err) ->
                [ div [] [ viewPatchSelect model ]
                , p [] [ text "Cannot load patch notes: ", text err ]
                ]


viewPage : Model -> List Change -> List (Html Msg)
viewPage model changes =
    let
        ( changesView, hasMore ) =
            viewChanges model (visibleChanges model changes)
    in
    [ viewFilters model
    , changesView
    , if hasMore then
        button [ class "more", onClick IncreasePageSize ] [ text "more" ]

      else
        text ""
    ]


visibleChanges : Model -> List Change -> List Change
visibleChanges model changes =
    let
        searchQuery =
            String.words model.searchTerm
                |> List.map String.toLower

        ( excludedDict, other ) =
            Dict.partition (\_ v -> v == Exclude) model.tagFilters

        ( requiredDict, _ ) =
            Dict.partition (\_ v -> v == Require) other

        excluded =
            Dict.keys excludedDict |> Set.fromList

        required =
            Dict.keys requiredDict |> Set.fromList

        isIncluded change =
            matchesTags change && matchesSearchTerm change

        matchesTags change =
            if Set.size (Set.intersect change.tags excluded) > 0 then
                False

            else
                Set.size (Set.intersect change.tags required) >= Set.size required

        matchesSearchTerm : Change -> Bool
        matchesSearchTerm change =
            if model.searchTerm == "" then
                True

            else
                let
                    doc =
                        change.date
                            :: change.text
                            :: Set.toList change.tags
                            |> List.map String.toLower

                    runQuery : List String -> ( List String, Bool )
                    runQuery terms =
                        case List.head terms of
                            Nothing ->
                                ( [], True )

                            Just term ->
                                if List.any (\text -> String.contains term text) doc then
                                    runQuery (List.tail terms |> Maybe.withDefault [])

                                else
                                    ( [], False )
                in
                runQuery searchQuery
                    |> Tuple.second
    in
    List.filter isIncluded changes


viewFilters : Model -> Html Msg
viewFilters model =
    let
        tagPill t =
            let
                ( prefix, invertedValue, extraClass ) =
                    case Dict.get t model.tagFilters of
                        Just Require ->
                            ( "−", Exclude, "plus" )

                        Just Exclude ->
                            ( "+", Require, "minus" )

                        _ ->
                            ( "", Ignore, "" )
            in
            span [ class <| "pill " ++ extraClass ]
                [ button
                    [ title "invert filter"
                    , onClick <| SetTagFilter t invertedValue
                    ]
                    [ text <| prefix ]
                , text <| " " ++ t ++ " "
                , button [ title "remove filter", onClick <| SetTagFilter t Ignore ] [ text "×" ]
                ]
    in
    div [ class "filters" ]
        [ div []
            [ viewPatchSelect model
            , input
                [ type_ "search"
                , placeholder "Search"
                , onInput SetSearchTerm
                , value model.searchTerm
                ]
                []
            ]
        , div [ class "active-tag-filters" ]
            (model.tagFilters
                |> Dict.keys
                |> List.map tagPill
            )
        ]


viewPatchSelect : Model -> Html Msg
viewPatchSelect model =
    let
        opt ( val, txt ) =
            option
                [ value val, selected <| model.patch == val ]
                [ text txt ]
    in
    select [ onInput SetPatch ] <|
        List.map opt
            [ ( "10.2", "Dragon Flight Season 3" )
            , ( "10.1", "Dragon Flight Season 2" )
            , ( "10.0", "Dragon Flight Season 1" )
            ]


tagFilterState : Model -> String -> TagFilterState
tagFilterState model tag =
    Dict.get tag model.tagFilters |> Maybe.withDefault Ignore


viewChanges : Model -> List Change -> ( Html Msg, Bool )
viewChanges model changes =
    let
        byDate : Dict ( String, String ) (List Change)
        byDate =
            List.foldr groupByDate Dict.empty changes

        groupByDate change dict =
            let
                key =
                    ( change.date, change.weekday )

                list : List Change
                list =
                    Dict.get key dict |> Maybe.withDefault []
            in
            Dict.insert key (change :: list) dict

        viewChangeSet ( ( date, weekday ), changeSet ) =
            div [] <|
                h2 [] [ text date, span [ class "weekday" ] [ text " ", text weekday ] ]
                    :: List.map (viewChange model) changeSet
    in
    if Dict.size byDate == 0 then
        ( p [] [ em [] [ text "No changes match the selected tags and/or search term." ] ]
        , False
        )

    else
        ( byDate
            |> Dict.toList
            |> List.reverse
            |> List.take model.pageSize
            |> List.map viewChangeSet
            |> div [ class "changes" ]
        , Dict.size byDate > model.pageSize
        )


viewChange : Model -> Change -> Html Msg
viewChange model change =
    div [ class "card" ]
        [ div [ class "tags" ]
            (Set.toList change.tags
                |> List.map (viewTagSwitch model)
            )
        , p [ class "text" ] [ text change.text ]
        , p [ class "source" ]
            [ a [ href change.url ] [ text "Source" ]
            ]
        ]


viewTagSwitch : Model -> String -> Html Msg
viewTagSwitch model tag =
    let
        enabled =
            tagFilterState model tag == Require
    in
    span [ class "pill " ]
        [ if enabled then
            button
                [ title <| "remove filter for " ++ tag
                , onClick (SetTagFilter tag Ignore)
                ]
                [ text "×" ]

          else
            button
                [ title ("show only changes tagged " ++ tag)
                , onClick (SetTagFilter tag Require)
                ]
                [ text "+" ]
        , text " "
        , span [ class "tag-text" ] [ text tag ]
        , text " "
        , button
            [ title ("hide changes tagged " ++ tag)
            , onClick (SetTagFilter tag Exclude)
            ]
            [ text "−" ]
        ]


type Msg
    = Nop
    | GotChanges (Result Http.Error (List Change))
    | SetPatch String
    | SetSearchTerm String
    | SendFiltersChanged String
    | SetTagFilter String TagFilterState
    | IncreasePageSize


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    let
        sendFiltersChanged ( newModel, cmd ) =
            ( newModel
            , Cmd.batch
                [ cmd
                , filtersChanged
                    { patch = newModel.patch
                    , searchTerm = newModel.searchTerm
                    , includeTags = Dict.filter (\_ v -> v == Require) newModel.tagFilters |> Dict.keys
                    , excludeTags = Dict.filter (\_ v -> v == Exclude) newModel.tagFilters |> Dict.keys
                    }
                ]
            )
    in
    case msg of
        Nop ->
            ( model, Cmd.none )

        GotChanges result ->
            case result of
                Ok changes ->
                    let
                        tagSet =
                            List.foldl
                                (\c s -> Set.union s c.tags)
                                Set.empty
                                changes
                    in
                    ( { model
                        | changes = Changes changes
                        , tagSet = tagSet
                        , tagFilters =
                            model.tagFilters
                                |> Dict.filter
                                    (\tag _ -> Set.member tag tagSet)
                      }
                    , Cmd.none
                    )

                Err err ->
                    ( { model | changes = Error err }, Cmd.none )

        SetPatch patch ->
            ( { model
                | patch = patch
                , changes = Loading
              }
            , loadChanges patch
            )
                |> sendFiltersChanged

        SetSearchTerm term ->
            ( { model | searchTerm = term }
            , setTimeout 100 (SendFiltersChanged term)
            )

        SendFiltersChanged ifTerm ->
            if model.searchTerm == ifTerm then
                ( model, Cmd.none ) |> sendFiltersChanged

            else
                ( model, Cmd.none )

        SetTagFilter tag state ->
            let
                filters =
                    model.tagFilters

                newFilters =
                    case state of
                        Ignore ->
                            Dict.remove tag filters

                        _ ->
                            Dict.insert tag state filters
            in
            ( { model | tagFilters = newFilters }, Cmd.none )
                |> sendFiltersChanged

        IncreasePageSize ->
            ( { model | pageSize = model.pageSize + 5 }, Cmd.none )


setTimeout : Float -> Msg -> Cmd Msg
setTimeout delay msg =
    Process.sleep delay
        |> Task.andThen (always <| Task.succeed msg)
        |> Task.perform identity


subscriptions : Model -> Sub Msg
subscriptions _ =
    Sub.none
