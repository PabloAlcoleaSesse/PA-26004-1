# Architecture

## Current foundation

One Go module contains three executable commands:

- `cmd/api`: HTTP endpoints with request timeouts and graceful shutdown.
- `cmd/worker`: River job processing with bounded concurrency and graceful draining.
- `cmd/admin`: explicit `migrate` and diagnostic `probe` commands.

Shared code lives in `internal/config`, `internal/platform`, `internal/httpapi`, and `internal/jobs`. The `internal/spotify` package owns OAuth, provider requests, and browser connection storage. PostgreSQL stores River's queue, migration history, and encrypted Spotify connections. Application migrations are embedded, checksummed, and applied under a transaction-scoped advisory lock.

The API and worker run as separate processes from the same container image. The image runs as a non-root user and includes CA certificates for future HTTPS integrations. Local Compose supplies PostgreSQL and runs migrations before starting either process. In other environments, run `admin migrate` as a single deployment step before starting application processes.

River's programmatic migrator uses the version pinned in `go.mod`; it is not downloaded independently at runtime. Migrations are forward-only through the supplied command. Backups and rollback procedures must be established for deployed environments before changing persistent application data.

The worker processes `system_probe`, `spotify_playlist_import`, `apple_music_playlist_import`, and `transfer_run` jobs. Import and transfer jobs carry only opaque IDs; credentials stay in encrypted PostgreSQL rows. Provider imports publish ordered catalog entries atomically. Transfer previews are persisted before any destination write is attempted, with each source entry classified as matched, ambiguous, missing, or unsupported.

## First music integration

Spotify account connection and Apple Music connection are implemented; see [the Spotify and Apple guide](spotify.md) for configuration, endpoints, tests, and current session models. Spotify uses PKCE and refreshable OAuth credentials. Apple uses MusicKit user tokens plus server-signed ES256 developer JWTs. Both providers use HttpOnly browser sessions and AES-GCM credential encryption. Real authorization still requires provider developer-app configuration and user consent.

Choose two services and verify the operations available to the app's actual credentials. Build provider packages around these verified capabilities. Authentication flows and supported playlist mutations may differ by service.

The first vertical slice should:

1. Connect accounts and store encrypted provider credentials outside job payloads.
2. Read a source playlist into an ordered snapshot, retaining repeated tracks.
3. Match destination recordings using available identifiers and metadata, recording confidence and manual decisions.
4. Preview a transfer, including unmatched tracks and version differences.
5. Create a destination playlist, persist progress, and reconcile interrupted operations before retrying.

The current implementation exposes `/api/transfers/previews` and `/api/transfers/previews/{id}` to create/read previews from Spotify snapshots. Transfer execution is queued through `/api/transfers/previews/{id}/runs`; Spotify can create a private destination playlist and Apple Music can create a library playlist, then both adapters append matched tracks with provider-specific identifiers. Unsupported provider operations remain explicit and are returned as controlled run failures.

The provider-neutral catalog is available through `GET /api/library/playlists`, `GET /api/library/playlists/{id}`, and `GET /api/library/stats`. Responses are scoped to the authenticated browser session. `GET /api/library/taste` summarizes persisted Spotify listening events; catalog coverage and listening history remain separate datasets. Spotify snapshots and queued Apple Music imports upsert canonical track metadata, provider source links, playlist occurrences, and unavailable or unsupported entries in one database transaction, so the catalog cannot expose a partially published import. Apple imports are queued through `POST /api/apple-music/imports` and polled through `GET /api/apple-music/imports/{id}`. Spotify history is queued through `POST /api/spotify/listening-imports` and polled through its status endpoint.

One-way playlist synchronization is queued through `POST /api/syncs` and monitored with `GET /api/syncs/{id}`. The worker resolves every source entry before writing, fails explicitly on missing, ambiguous, unavailable, or unsupported entries, and re-reads the destination position before each append so a retry after an uncertain remote write does not duplicate tracks.

Application identity is stored separately from provider credentials. Successful provider connection rotations create or reuse an application user, move the session mapping, and link an opaque provider account identifier in the same transaction as the encrypted connection. `GET /api/me` returns the durable user ID and linked provider names without exposing account tokens or token fingerprints.

Keep matching and transfer planning independent of provider HTTP clients so their rules can be tested with fixtures. Introduce shared track and playlist types as the first integrations reveal the required fields.

## Sync reliability

Treat external mutations as potentially repeated. River retries do not make a remote API call and a local database update atomic. A process can stop after a provider accepted a write but before local progress was saved. Reconcile the destination before retrying an uncertain write.

Persist a sync request and enqueue its job in the same PostgreSQL transaction. Serialize updates to each destination playlist across worker processes. Add per-provider rate limiting, cancellation, request deadlines, and retry handling when implementing adapters; queue concurrency alone does not enforce provider rate limits.

Begin with one-way synchronization and an explicit source of truth. Add bidirectional synchronization only after defining conflict rules for additions, deletions, and ordering.

## Deployment boundary

This is a development foundation with browser sessions, encrypted credentials, persistent application identity, and queued one-way synchronization. Deployment still needs managed secrets/key rotation, TLS termination, database backups, abuse controls, and monitoring. JSON application logs and River logs exist today; metrics and distributed tracing are future work. The public repository does not imply public access has been approved by any music provider.

## References

- [Go documentation](https://go.dev/doc/)
- [River documentation](https://riverqueue.com/docs)
- [River migrations](https://riverqueue.com/docs/migrations)
- [River graceful shutdown](https://riverqueue.com/docs/graceful-shutdown)
