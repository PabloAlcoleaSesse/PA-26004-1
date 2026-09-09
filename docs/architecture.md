# Architecture

## Current foundation

One Go module contains three executable commands:

- `cmd/api`: HTTP endpoints with request timeouts and graceful shutdown.
- `cmd/worker`: River job processing with bounded concurrency and graceful draining.
- `cmd/admin`: explicit `migrate` and diagnostic `probe` commands.

Shared code lives in `internal/config`, `internal/platform`, `internal/httpapi`, and `internal/jobs`. The `internal/spotify` package owns OAuth, provider requests, and browser connection storage. PostgreSQL stores River's queue, migration history, and encrypted Spotify connections. Application migrations are embedded, checksummed, and applied under a transaction-scoped advisory lock.

The API and worker run as separate processes from the same container image. The image runs as a non-root user and includes CA certificates for future HTTPS integrations. Local Compose supplies PostgreSQL and runs migrations before starting either process. In other environments, run `admin migrate` as a single deployment step before starting application processes.

River's programmatic migrator uses the version pinned in `go.mod`; it is not downloaded independently at runtime. Migrations are forward-only through the supplied command. Backups and rollback procedures must be established for deployed environments before changing persistent application data.

The worker currently processes only `system_probe` jobs. Probe completion proves that a job was persisted, picked up, and acknowledged. No endpoint accepts public sync requests yet.

## First music integration

Spotify account connection is implemented; see [the Spotify guide](spotify.md) for its configuration, endpoints, tests, and current session model. The flow uses PKCE, single-use state, HttpOnly session cookies, AES-GCM credential encryption, and serialized refreshes. Real account authorization still requires developer-app configuration and browser consent. Pending OAuth flows currently require one API instance.

Choose two services and verify the operations available to the app's actual credentials. Build provider packages around these verified capabilities. Authentication flows and supported playlist mutations may differ by service.

The first vertical slice should:

1. Connect accounts and store encrypted provider credentials outside job payloads.
2. Read a source playlist into an ordered snapshot, retaining repeated tracks.
3. Match destination recordings using available identifiers and metadata, recording confidence and manual decisions.
4. Preview a transfer, including unmatched tracks and version differences.
5. Create a destination playlist, persist progress, and reconcile interrupted operations before retrying.

Keep matching and transfer planning independent of provider HTTP clients so their rules can be tested with fixtures. Introduce shared track and playlist types as the first integrations reveal the required fields.

## Sync reliability

Treat external mutations as potentially repeated. River retries do not make a remote API call and a local database update atomic. A process can stop after a provider accepted a write but before local progress was saved. Reconcile the destination before retrying an uncertain write.

Persist a sync request and enqueue its job in the same PostgreSQL transaction. Serialize updates to each destination playlist across worker processes. Add per-provider rate limiting, cancellation, request deadlines, and retry handling when implementing adapters; queue concurrency alone does not enforce provider rate limits.

Begin with one-way synchronization and an explicit source of truth. Add bidirectional synchronization only after defining conflict rules for additions, deletions, and ordering.

## Deployment boundary

This is a development foundation with Spotify browser sessions and encrypted credentials. Before unattended multi-provider sync, introduce a persistent application-user model and account-linking authorization. Deployment also needs managed secrets/key rotation, TLS termination, database backups, abuse controls, and monitoring. JSON application logs and River logs exist today; metrics and distributed tracing are future work. The public repository does not imply public access has been approved by any music provider.

## References

- [Go documentation](https://go.dev/doc/)
- [River documentation](https://riverqueue.com/docs)
- [River migrations](https://riverqueue.com/docs/migrations)
- [River graceful shutdown](https://riverqueue.com/docs/graceful-shutdown)
