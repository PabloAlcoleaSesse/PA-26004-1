# Repository agent instructions

## Project context

This repository is a Go backend for moving music libraries between services. The current providers are Spotify and Apple Music. Spotify supports OAuth PKCE, encrypted credentials, playlist pagination, and ordered playlist snapshot imports. Apple Music supports MusicKit user tokens, ES256 developer tokens, encrypted credentials, and playlist listing. Cross-service matching, transfer previews, and destination writes are the next product milestones.

Read the relevant package, migration, and documentation before changing behavior. Keep the provider-specific implementations in `internal/spotify` and `internal/applemusic`; put shared transfer concepts behind provider-neutral interfaces and models.

## Overnight implementation objective

Advance the transfer pipeline in small, reviewable steps:

1. Add a provider-neutral track and playlist representation.
2. Build deterministic matching using provider IDs, ISRC when available, normalized artist/title metadata, and duration. Preserve source order and duplicates.
3. Classify results as matched, ambiguous, missing, or unsupported. Never silently choose a low-confidence match.
4. Add a preview endpoint and persistence for a proposed transfer before writing to a destination service.
5. Add destination writes behind the provider interface (`list playlists`, `read entries`, `search`, `create playlist`, and `add entries`). Queue long-running work through River.
6. Make transfer jobs idempotent, rate-limit aware, retry-safe, and observable. Persist enough state to resume safely after a crash or an uncertain remote write.

Do not invent provider capabilities. Confirm Spotify and Apple Music behavior against their official documentation before adding an API call, and keep unsupported operations explicit in the API.

## Security and reliability

- Never log, return, or put access tokens, refresh tokens, private keys, or raw authorization codes in job payloads. River payloads should contain opaque database IDs only.
- Keep stored credentials encrypted with `TOKEN_ENCRYPTION_KEY`; validate configuration at startup.
- Preserve OAuth state, PKCE, session, and CSRF protections. Use safe, stable error codes at API boundaries.
- Do not trust provider-controlled pagination URLs. Enforce context deadlines, bounded page sizes, retry limits, and `Retry-After` handling.
- Prevent duplicate active transfers for the same source and destination. Serialize writes to a target playlist where ordering requires it.
- Treat remote-write/local-commit uncertainty as a first-class state. Reconciliation must be possible without duplicating tracks.
- Preserve unavailable tracks and report them instead of silently dropping them.

## Tests and local checks

Run the focused tests while iterating, then run the full checks before publishing:

```sh
gofmt -w cmd internal
go vet ./...
go test -race -count=1 ./...
go build ./...
```

`make check` is the preferred equivalent when available. Database integration tests may use an explicitly supplied `TEST_DATABASE_URL`; isolate test data with a dedicated schema or database and clean it up afterward. Mock provider clients for matching, pagination, rate limits, retries, partial failures, duplicate entries, and restart/reconciliation cases.

Do not start Docker or compose services during normal development or checks. Container smoke tests are opt-in only through the workflow's manual dispatch input. If a database is needed locally, use an explicitly managed temporary instance and stop it when the test ends.

## Documentation and code quality

Update `README.md` and the relevant `docs/` guide when behavior, configuration, API contracts, or operational steps change. Add comments around security-sensitive, concurrency-sensitive, and provider-specific decisions; avoid comments that merely restate code.

Keep handlers thin, validate input at the boundary, pass `context.Context` through provider and storage calls, and keep migrations forward-only with checksum coverage in `internal/platform/migrations.go`.

## Git hygiene and completion

- Preserve unrelated user changes and untracked files. Stage only files required for the current task.
- Never commit `.env` files, credentials, private keys, account data, or generated secrets.
- Prefer focused conventional commits. Do not rewrite shared history or force-push.
- Before reporting completion, run the applicable checks, inspect the diff, and push the completed commit to the configured public remote.
- Report what changed, what was tested, any known limitations, and any follow-up work still needed.
