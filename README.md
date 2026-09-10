# PA-26004-1

A platform to keep your music library connected across music services.

Save your collection, transfer playlists between services, and keep them in sync. Over time, explore your music taste through statistics and discover recommendations based on how music sounds.

## Project status

Go backend with an HTTP API, River worker, PostgreSQL migrations, and optional Spotify and Apple Music connections. Spotify and Apple Music OAuth or MusicKit connections, encrypted credentials, playlist pagination, ordered snapshot imports, transfer previews, and destination playlist writes are implemented. Cross-service synchronization and a separate application-user system are not implemented yet. See [Spotify setup](docs/spotify.md) for provider configuration.

## Vision

- **One library:** keep a unified collection of saved tracks, albums, and playlists with links to their services.
- **Transfers:** move playlists and saved music between services, with clear reporting for missing or uncertain matches.
- **Sync:** keep selected playlists aligned across connected accounts, with control over changes.
- **Taste statistics:** understand listening patterns and how your preferences evolve.
- **Sound-based discovery:** explore recommendations informed by musical and audio characteristics.

Transfers refer to recreating library entries and playlists in a destination service. Service support and available features will depend on each integration's capabilities.

## Roadmap

- [ ] Define the first two music services and investigate integration capabilities.
- [x] Establish the Go, PostgreSQL, and River application foundation.
- [ ] Design a shared music-library model around the first two integrations.
- [x] Implement Spotify account connection with encrypted credentials.
- [x] Add Apple Music connection and playlist listing through MusicKit user tokens.
- [ ] Verify a live Spotify connection with developer-app credentials and consent.
- [x] Import Spotify playlist metadata and ordered snapshots.
- [x] Match tracks across services and preview a playlist transfer.
- [x] Execute Spotify destination playlist creation and track mutations.
- [x] Execute Apple Music destination playlist creation and track mutations.
- [ ] Add opt-in playlist synchronization with conflict handling.
- [ ] Add music-taste statistics using available data.
- [ ] Explore sound-based recommendations and evaluate their quality.

## Development

### Run with Docker

Requires Docker with Compose v2 or newer. From the repository root:

```sh
docker compose up --build -d --wait
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
docker compose exec -T worker /app/admin probe
```

Compose starts PostgreSQL, runs River migrations, then starts the API and worker. The probe command inserts a diagnostic job and waits up to one minute for the worker to complete it. It does not connect to a music service.

The API is available at `http://127.0.0.1:8080`. PostgreSQL is exposed on `127.0.0.1:5433` to avoid the usual local PostgreSQL port. Both ports are bound to loopback. Override `API_PORT` and `POSTGRES_PORT` if needed.

```sh
docker compose logs -f api worker
docker compose down
```

Stopping the stack preserves the database volume. The credentials in Compose are for local development only.

### Run Go processes locally

Requires Go 1.26.6 or newer and an available PostgreSQL instance (16 or 17). Copy `.env.example` to `.env` if it does not exist, then set `DATABASE_URL` for your database. Load configuration in each terminal:

```sh
set -a
. ./.env
set +a
make migrate
make api
```

In another terminal, load `.env` as above and run `make worker`. In a third terminal, load `.env` and run `make probe`. The Go commands read environment variables; they do not automatically load `.env`. Avoid running the local API and container API on the same port.

### Checks

```sh
make check
```

Checks formatting, runs `go vet`, runs tests with the race detector, and builds `bin/api`, `bin/worker`, and `bin/admin`. These checks use simulated provider clients and do not start containers or contact Spotify/Apple Music. PostgreSQL integration tests are opt-in through `TEST_DATABASE_URL`; see [verification](docs/spotify.md#verification).

GitHub Actions runs the Go checks on pushes and pull requests. The container smoke test runs only when manually dispatching CI with `container_smoke` enabled.

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL connection URL |
| `HTTP_ADDR` | `127.0.0.1:8080` | API listen address |
| `LOG_LEVEL` | `info` | JSON log verbosity: debug, info, warn, error |
| `WORKER_CONCURRENCY` | `4` | Concurrent jobs per worker process, from 1 to 100 |
| `SPOTIFY_CLIENT_ID` | Unset (disabled) | Enable Spotify OAuth using this app's client ID |
| `SPOTIFY_REDIRECT_URI` | Required when enabled | Registered callback URL |
| `TOKEN_ENCRYPTION_KEY` | Required when enabled | Base64-encoded 32-byte credential encryption key |
| `APPLE_TEAM_ID` | Unset (disabled) | Apple Developer Team ID |
| `APPLE_KEY_ID` | Required when enabled | Apple MusicKit key ID |
| `APPLE_PRIVATE_KEY` | Required when enabled | PKCS#8 ES256 private key PEM |
| `PUBLIC_ORIGIN` | `http://127.0.0.1:8080` | Origin allowed for Apple Music mutations |

`GET /healthz` reports whether the HTTP process is alive. `GET /readyz` checks database connectivity and access to the River and Spotify tables, returning `503` when unavailable. Neither endpoint establishes that the separate worker is running; use the probe command for that.

See [the architecture notes](docs/architecture.md) for module boundaries and the next implementation steps.

Ideas and contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Licensing

A license has not been selected yet.
