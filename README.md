# PA-26004-1

A platform to keep your music library connected across music services.

Save your collection, transfer playlists between services, and keep them in sync. Over time, explore your music taste through statistics and discover recommendations based on how music sounds.

## Project status

Backend foundation implemented in Go: an HTTP API, a River background worker, PostgreSQL migrations, and a local container environment. Music-service integrations, user authentication, and playlist synchronization are not implemented yet.

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
- [ ] Connect an account and import playlist metadata.
- [ ] Match tracks across services and preview a playlist transfer.
- [ ] Execute transfers and report successful, missing, and ambiguous matches.
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

Requires Go 1.26.6 or newer and PostgreSQL 17 (or the supplied database container). Copy and load the example configuration in each terminal:

```sh
cp .env.example .env
set -a
. ./.env
set +a
docker compose up -d db
make migrate
make api
```

In another terminal, load `.env` as above and run `make worker`. In a third terminal, load `.env` and run `make probe`. The Go commands read environment variables; they do not automatically load `.env`. Avoid running the local API and container API on the same port.

### Checks

```sh
make check
```

Checks formatting, runs `go vet`, runs tests with the race detector, and builds `bin/api`, `bin/worker`, and `bin/admin`. GitHub Actions also builds the containers and exercises health checks, queue delivery, and repeatable migrations against PostgreSQL.

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DATABASE_URL` | Required | PostgreSQL connection URL |
| `HTTP_ADDR` | `127.0.0.1:8080` | API listen address |
| `LOG_LEVEL` | `info` | JSON log verbosity: debug, info, warn, error |
| `WORKER_CONCURRENCY` | `4` | Concurrent jobs per worker process, from 1 to 100 |

`GET /healthz` reports whether the HTTP process is alive. `GET /readyz` checks database connectivity and access to the River jobs table, returning `503` when unavailable. Neither endpoint establishes that the separate worker is running; use the probe command for that.

See [the architecture notes](docs/architecture.md) for module boundaries and the next implementation steps.

Ideas and contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Licensing

A license has not been selected yet.
