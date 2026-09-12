# API contract

All JSON endpoints use the `music_session` HttpOnly cookie established by a
provider connection. Provider credentials and session hashes are never
accepted in request bodies. Mutating endpoints that change a connection,
transfer, or sync require an `Origin` matching `PUBLIC_ORIGIN`.

## Identity and catalog

`GET /api/me` returns the durable application user and linked provider names.
It returns `401` when the session is not connected.

`GET /api/library/stats` returns imported catalog coverage:

```json
{"playlists":2,"entries":84,"tracks":76,"unavailable":1,"unsupported":2,"by_provider":{"spotify":1,"apple-music":1}}
```

`GET /api/library/taste?limit=10` returns aggregate listening history after a
Spotify listening import. It reports play counts and top tracks/artists; an
empty result is expected until history has been imported.

`GET /api/library/playlists?limit=100` lists the session's imported playlists.
`GET /api/library/playlists/{id}?offset=0&limit=100` returns playlist metadata
and ordered entries. Unavailable and unsupported entries remain in the result
with a null `track` value and their status flags set.

## Imports

Spotify imports are started with
`POST /api/spotify/playlists/{playlist_id}/imports` and expose status and
snapshot URLs in the response. Apple Music imports use:

```http
POST /api/apple-music/imports
Content-Type: application/json

{"playlist_id":"p.example"}
```

The response is `202` with an opaque `id`; poll
`GET /api/apple-music/imports/{id}` until `state` is `completed` or `failed`.

Spotify recently-played history is imported with
`POST /api/spotify/listening-imports` and polled through the returned status
URL. The importer requests at most 50 recent events per job and uses a stable
provider event key, so repeated polling does not duplicate plays.

## Transfers and synchronization

Transfer previews are created with `POST /api/transfers/previews`, read with
`GET /api/transfers/previews/{id}`, and executed through
`POST /api/transfers/previews/{id}/runs`. Every preview entry is classified as
`matched`, `ambiguous`, `missing`, or `unsupported` before a destination write.

An ambiguous entry can be resolved by selecting one of its persisted
destination candidates:

```http
POST /api/transfers/previews/{preview_id}/entries/{position}/match
Content-Type: application/json

{"provider":"apple-music","id":"candidate-id"}
```

The candidate must exactly match a candidate returned in that preview and the
request must belong to the preview's session. The response returns the updated
entry with `status: "matched"` and `reason: "manual_match"`. Match decisions
are durable and are rejected once a transfer run exists, preventing changes
while destination writes may be in progress.

One-way synchronization is queued with:

```json
POST /api/syncs
{"source_provider":"spotify","source_playlist_id":"p1","destination_provider":"apple-music","destination_playlist_id":"","schedule_interval_seconds":3600}
```

The response is `202` and can be polled with `GET /api/syncs/{id}`. A sync
fails explicitly on ambiguous, missing, unavailable, or unsupported source
entries; it does not silently drop them. A blank destination ID creates or
reuses a playlist named `<source> (sync)`. `schedule_interval_seconds` is
optional; zero creates a one-shot sync, while recurring syncs accept intervals
from 900 seconds (15 minutes) through 2592000 seconds (30 days). A recurring
sync runs immediately and schedules its next run after successful completion.
`DELETE /api/syncs/{id}` disables future runs and returns the current status.

Provider errors use stable HTTP classes: `401` for a missing or expired
connection, `403` for an origin or provider authorization failure, `404` for a
missing resource, `409` for a state conflict where applicable, `429` for a
provider rate limit, and `5xx` for transient service failures.
