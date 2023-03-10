port module Main exposing (main)

import Browser
import Date
import Dict exposing (Dict)
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Http
import Json.Decode as Decode
import Process
import Set exposing (Set)
import Task
import Time


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
    { changes : Maybe (List Change)
    , pageSize : Int
    , tagSet : Set String
    , searchTerm : String
    , tagFilters : Dict String TagFilterState

    -- , stagedFilterState : TagFilterState
    -- , stagedFilterTag : String
    }


type TagFilterState
    = Require
    | Exclude
    | Ignore


type alias Change =
    { date : String
    , weekday : String
    , tags : Set String
    , text : String
    }


changeDecoder : Decode.Decoder Change
changeDecoder =
    Decode.map4 Change
        (Decode.field "Date" Decode.string)
        (Decode.field "Weekday" Decode.string)
        (Decode.field "Tags" (Decode.list Decode.string |> Decode.map Set.fromList))
        (Decode.field "Change" Decode.string)


type alias Args =
    { searchTerm : String
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
    ( { changes = Nothing
      , pageSize = 5
      , tagSet = Set.empty
      , searchTerm = args.searchTerm
      , tagFilters =
            Dict.empty
                |> Dict.union (tags .excludeTags Exclude)
                |> Dict.union (tags .includeTags Require)

      -- , stagedFilterState = Require
      -- , stagedFilterTag = ""
      }
    , Http.get
        { url = "./wow-patch-notes.json"
        , expect = Http.expectJson GotChanges (Decode.list changeDecoder)
        }
    )


view : Model -> Html Msg
view model =
    div []
        (case model.changes of
            Nothing ->
                [ p [] [ text "Loading …" ] ]

            Just changes ->
                let
                    ( changesView, hasMore ) =
                        viewChanges model (visibleChanges model changes)
                in
                [ div [ class "change-list" ]
                    [ h1 [] [ text "Patch Notes" ]
                    , viewFilters model
                    , changesView
                    , if hasMore then
                        button [ class "more", onClick IncreasePageSize ] [ text "more" ]

                      else
                        text ""
                    ]
                ]
        )


visibleChanges : Model -> List Change -> List Change
visibleChanges model changes =
    let
        searchQuery =
            String.words model.searchTerm
                |> List.map String.toLower

        ( excludedDict, other ) =
            Dict.partition (\k v -> v == Exclude) model.tagFilters

        ( requiredDict, _ ) =
            Dict.partition (\k v -> v == Require) other

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
                                ( [], False )

                            Just term ->
                                if List.any (\text -> String.contains term text) doc then
                                    ( [], True )

                                else
                                    runQuery (List.tail terms |> Maybe.withDefault [])
                in
                runQuery searchQuery
                    |> Tuple.second
    in
    List.filter isIncluded changes


viewFilters model =
    let
        tagPill t =
            let
                ( prefix, invertedValue, extraClass ) =
                    case Dict.get t model.tagFilters of
                        Just Require ->
                            ( "+", Exclude, "plus" )

                        Just Exclude ->
                            ( "−", Require, "minus" )

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

        -- tagOption t =
        --     option
        --         [ value t, selected (model.stagedFilterTag == t) ]
        --         [ text t ]
    in
    div [ class "filters" ]
        [ div []
            [ input
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

        -- , div [ class "new-tag-filter" ]
        --     [ select [ onInput SetStagedFilterState ]
        --         [ option
        --             [ value "Require", selected (model.stagedFilterState /= Ignore) ]
        --             [ text "Show only" ]
        --         , option
        --             [ value "Exclude", selected (model.stagedFilterState == Exclude) ]
        --             [ text "Hide" ]
        --         ]
        --     , text " changes tagged with "
        --     , select [ onInput SetStagedFilterTag ] <|
        --         List.map tagOption <|
        --             Set.toList <|
        --                 model.tagSet
        --     , text " "
        --     , button [ onClick ApplyStagedFilter ]
        --         [ text "OK" ]
        --     ]
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
        [ p [ class "tags" ]
            (Set.toList change.tags
                |> List.map (viewTagSwitch model)
            )
        , p [ class "text" ]
            [ text change.text ]
        ]


viewTagSwitch : Model -> String -> Html Msg
viewTagSwitch model tag =
    let
        ( ( plusClass, plusState ), ( minusClass, minusState ) ) =
            case tagFilterState model tag of
                Require ->
                    ( ( "active", Ignore ), ( "", Exclude ) )

                Exclude ->
                    ( ( "", Require ), ( "active", Ignore ) )

                Ignore ->
                    ( ( "", Require ), ( "", Exclude ) )
    in
    span [ class "pill" ]
        [ button
            [ class ("plus " ++ plusClass)
            , title ("show only changes tagged " ++ tag)
            , onClick (SetTagFilter tag plusState)
            ]
            [ text "+" ]
        , text " "
        , text tag
        , text " "
        , button
            [ class ("minus " ++ minusClass)
            , title ("hide changes tagged " ++ tag)
            , onClick (SetTagFilter tag minusState)
            ]
            [ text "−" ]
        ]


type Msg
    = Nop
    | GotChanges (Result Http.Error (List Change))
    | SetSearchTerm String
    | SendFiltersChanged String
      -- | SetStagedFilterState String
      -- | SetStagedFilterTag String
      -- | ApplyStagedFilter
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
                    { searchTerm = newModel.searchTerm
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
                        | changes = Just changes
                        , tagSet = tagSet
                        , tagFilters =
                            model.tagFilters
                                |> Dict.filter
                                    (\tag _ -> Set.member tag tagSet)

                        -- , stagedFilterTag =
                        --     tagSet
                        --         |> Set.toList
                        --         |> List.head
                        --         |> Maybe.withDefault ""
                      }
                    , Cmd.none
                    )

                Err _ ->
                    ( { model | changes = Nothing }, Cmd.none )

        SetSearchTerm term ->
            ( { model | searchTerm = term }
            , setTimeout 100 (SendFiltersChanged term)
            )

        SendFiltersChanged ifTerm ->
            if model.searchTerm == ifTerm then
                ( model, Cmd.none ) |> sendFiltersChanged

            else
                ( model, Cmd.none )

        -- SetStagedFilterTag t ->
        --     ( { model | stagedFilterTag = t }, Cmd.none )
        -- SetStagedFilterState stateStr ->
        --     case stateStr of
        --         "Require" ->
        --             ( { model | stagedFilterState = Require }, Cmd.none )
        --         "Exclude" ->
        --             ( { model | stagedFilterState = Exclude }, Cmd.none )
        --         _ ->
        --             ( { model | stagedFilterState = Ignore }, Cmd.none )
        -- ApplyStagedFilter ->
        --     update
        --         (SetTagFilter model.stagedFilterTag
        --             model.stagedFilterState
        --         )
        --         model
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
subscriptions model =
    Sub.none
