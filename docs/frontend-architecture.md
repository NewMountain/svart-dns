# Frontend state and effect boundaries

`AppStateProvider` owns the application's state tree. `appReducer` is the established
update path for UI fields, API resources, configuration invalidation, and visible
operation errors. Router URLs remain the canonical source for navigation, selected
entities, and shareable filters.

`UiFieldValues` names each feature's state with its concrete TypeScript type.
`useUiField` selects a mounted view's field and dispatches pure immutable transitions;
its instance key isolates simultaneous mounts and cleanup removes their fields.
`useUiReducer` composes the existing Setup and configuration-backup reducers through
the same root transition. Transition functions must contain no I/O or mutation.

API resources also live in the root tree. `useApi` owns request scheduling and
cancellation guards, while `apiReducer` handles tagged idle, loading, ready, and
failed states. Refresh failures preserve the last successfully decoded data.
A path and concrete resource key identify each mounted resource; changing them cannot
show an unrelated resource's result. The explicit resource registry derives each
payload type from its generated decoder. The mapped root tree retains that key/payload
correlation through storage, selection, and immutable request transitions. Decoders
run only at the HTTP boundary; selectors trust the typed model without parsing again.
`NoInfer` on transition payloads prevents a wrong payload from widening the chosen key.

The `uiUpdated` and `resourcesUpdated` messages carry pure transition functions rather
than serialized events. They compose concrete feature transitions through the single
root reducer, preserving TypeScript's key/payload correlation without casts. These
functions only read their previous state and captured, already-decoded values; network
work, clock reads, error conversion, and scheduling happen before dispatch at the
controller edge. They never mutate captured objects or perform I/O.

Admin relative-time labels use a timestamp stored in the root UI state. Its controller
samples the clock on mount and once per minute in an effect; rendering the same model
always produces the same relative-time label, even if the ambient clock changes.

Route and component controller hooks own network requests, timers, browser effects,
and event actions. Exported components connect these controllers to pure views that
receive typed models and actions. DOM host references, such as the dashboard's
intersection-observer anchor, stay at the effect boundary; they are not application
state. A controller must await a mutation before clearing a draft or refreshing
persisted data. `useAction` routes failures to the root error state and
`ApplicationStatus`; failed forms retain their input for retry.

Configuration imports use the secured existing APIs, then fetch the saved export to
verify the result. Backup comparison preserves bootstrap-server order because that
order controls resolver fallback. Entity and association collections that represent
sets compare independently of their export order.

Run `npm run verify` in `frontend` for pinned formatting, strict type-aware lint,
TypeScript checking, the full test suite with four 80% coverage gates over all
production frontend modules, and the production build. Generated API artifacts are
formatted by the generator's update and check paths using the same pinned Prettier;
do not edit them directly. Browser preservation evidence additionally requires the
authenticated desktop/mobile control inventory, lower-page screenshots, and real
mutation/readback flows; unit coverage does not replace those checks.
