# Search, history and Activity in the codeaf app

## Jump anywhere, command K, search agents crews jobs and files

The app's Jump Anywhere field calls `GET /app/v1/search`. Search uses plain
text; it makes no AI calls. The binary returns Agents and crews, Jobs, and
Files, in that order, with at most four rows per group. Title prefixes come
first, then word prefixes, then substrings; equal ranks use the latest known
use time and a stable id. Tabs and Commands belong to the app.

Search normally omits closed and archived agents and crews. Choose
“Search closed and archived too” to include them. Solo jobs, including Done,
come from the mounted factory's queue with the caller's access checks.
Team jobs come from the app's team-factory cache, not the binary's search.
Files use agents' existing context chips and their workspace file-finding door;
there is no unrestricted scan of this Mac. While that door is unavailable,
only existing context chips are searched. Missing chips are excluded.

## All crews and agents, saved history, said since model and cost filters

`GET /app/v1/history` keeps open, pinned, closed and archived agents and crews.
`GET /app/v1/agents?q=` accepts the same filters for agent rows. Plain words
match names (and a crew's goal). All terms must match. Quote a phrase, for
example `said:"billing timeout"` or `crew:"Release work"`.

Filters are `state:`, `said:`, `since:`, `kind:`, `crew:`, `model:`, `cost:` and
`pinned:true|false`. `state:open` excludes Closed and Archived;
`state:pinned` selects pinned agents. `kind:agent` or `kind:crew` selects a
resource. Crew filters match its id or name; a crew's model is its manager's.
`said:` searches saved and live user/assistant messages, including earlier
compacted history, and crew members' messages and reports. It does not search
tool output or receipts and never opens a model connection.

`since:2026-10-01` is a UTC date; `since:7d` means the preceding seven days.
`cost:>=4.20` compares actual recorded spend in exact cents; `>`, `<`, `<=` and
`=` work too. Unknown spend cannot match a cost filter. Unsupported filters,
invalid dates or amounts, and unfinished quotes return 422 `bad_input`.

## App Activity, tidy actions and undo history

`GET /app/v1/activity?since=` lists the engine's recorded tidy actions, newest
first. `since` accepts a UTC date or RFC3339 timestamp and excludes actions at
or before that instant. Successful agent close, archive, reopen and pin changes
are recorded; crew finish/archive/reopen, rule removal/restoration and memory
forget/restoration are recorded when their owning resource supports them.
Each record contains its time, actor, verb, object reference and detail, and
emits an `activity` event. Failed actions and unchanged retries add no line.
Activity survives an engine restart in the shared home.

Tab tidying and its undo-until-quit stack stay in the app. Activity itself
cannot undo an action: the app calls the source's reopen or restore verb where
available. A resource whose route still returns 501 cannot perform that action.
