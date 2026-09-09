# Connecting Spotify

## What is implemented

The API supports Spotify OAuth with PKCE, encrypted credential storage, automatic refresh when checking the connection, and local disconnect. It requests `playlist-read-private` and `playlist-read-collaborative` for the upcoming playlist import. Playlist reading, transfers, and scheduled sync are not implemented yet.

Spotify authorization establishes a browser session for this first integration. There is no separate application user/login system yet. Each browser connection is independent; a second browser must authorize separately.

## Configure the developer app

1. Create an app in the [Spotify developer dashboard](https://developer.spotify.com/dashboard) and select Web API access.
2. Register this exact redirect URI: `http://127.0.0.1:8080/auth/spotify/callback`.
3. Add your test account to the app's allowed users if required by its development-mode settings.
4. Put the client ID in your local `.env`. PKCE does not require a client secret.
5. Generate a token-encryption key locally with `openssl rand -base64 32` and put the result in `.env` as `TOKEN_ENCRYPTION_KEY`. Keep it private and stable across restarts.

```dotenv
SPOTIFY_CLIENT_ID=your_client_id
SPOTIFY_REDIRECT_URI=http://127.0.0.1:8080/auth/spotify/callback
TOKEN_ENCRYPTION_KEY=your_generated_base64_key
```

When `SPOTIFY_CLIENT_ID` is unset, the integration routes are not registered and the rest of the API remains available. Once it is set, the API refuses to start with an invalid redirect URI or encryption key.

Spotify requires HTTPS except for explicit loopback IP addresses such as `127.0.0.1` or `[::1]`; `localhost` is not allowed. If you change the port, update the registered redirect URI, `SPOTIFY_REDIRECT_URI`, and `HTTP_ADDR` together. New development apps currently require the owner to have Spotify Premium and support at most five allowed users. See the official [redirect rules](https://developer.spotify.com/documentation/web-api/concepts/redirect_uri) and [development-mode restrictions](https://developer.spotify.com/documentation/web-api/tutorials/february-2026-migration-guide).

## Run when ready

Use an available PostgreSQL instance and set `DATABASE_URL` to its connection URL. These commands do not start containers. Do not overwrite an existing `.env`; copy `.env.example` only if you have not already created one.

```sh
set -a
. ./.env
set +a
make migrate
make api
```

The migration command applies River migrations and the application schema. Open `http://127.0.0.1:8080/auth/spotify` in your browser and approve Spotify's consent screen. The callback redirects to `/api/connections/spotify`, which returns:

```json
{
  "connected": true,
  "profile": {
    "account_id": "your_stable_spotify_account_id",
    "display_name": "Your display name"
  }
}
```

The worker is not required for account connection. No access or refresh token appears in the response. You can revisit the connection endpoint in the same browser; it checks the profile against Spotify and refreshes expired credentials as needed.

## Endpoints

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/auth/spotify` | Start browser authorization |
| GET | `/auth/spotify/callback` | Validate state, exchange the code, store credentials, rotate the local session |
| GET | `/api/connections/spotify` | Check the current browser's connection and return its Spotify profile |
| DELETE | `/api/connections/spotify` | Delete this browser's stored credentials and expire its session cookie |

Disconnect requires the session cookie and an `Origin` header matching the redirect URI's origin. From the browser console on the API's origin:

```js
fetch('/api/connections/spotify', { method: 'DELETE' })
```

This disconnect removes the local connection only. To revoke the app's Spotify grant, remove it from [your Spotify apps](https://www.spotify.com/account/apps/). Other browser connections are independent.

An absent/expired session or revoked grant returns `401`; Spotify access denial returns `403`; rate limiting returns `429` with a safe `Retry-After` value when supplied. Other upstream failures return `502`. Authorization denial or an invalid/expired callback returns `400`. Error responses omit provider response bodies and credentials.

## How the security mechanisms fit together

- **PKCE:** the server retains a random verifier and sends its SHA-256 challenge to Spotify. The code exchange proves possession of the verifier.
- **State and cookies:** each authorization requires matching random state and a browser cookie. Attempts expire after ten minutes and can be consumed once.
- **Sessions:** the browser receives an opaque, HttpOnly, SameSite=Lax cookie. HTTPS configurations also set Secure. PostgreSQL stores only its SHA-256 hash; sessions expire after 30 days and rotate on reconnection.
- **Encryption:** account metadata and both tokens are encrypted with AES-256-GCM. A fresh nonce is generated for each write, and the session hash is authenticated to prevent ciphertext substitution between sessions.
- **Refresh:** PostgreSQL row locks serialize concurrent refresh attempts for a session. An omitted replacement refresh token retains the old value. A successful rotation is committed even if the following profile request fails, provided the database transaction can still commit.

Authorization attempts are held in a bounded in-memory map, so a restart requires restarting any pending authorization. Run one API instance for the current OAuth flow; distributed attempt storage is required before scaling it across instances. Established connections survive API restarts with the same encryption key. Losing/changing the key requires reconnecting. Expired rows are removed on the next successful connection; periodic cleanup and key rotation are future work.

A provider token refresh and a database commit cannot be atomic. If the process or database fails after Spotify rotates a token, reconnecting may be necessary. The current implementation also treats the browser session as the connection owner; a persistent application-user model must precede unattended multi-provider sync.

## Verification

`make check` runs the mock-provider tests, encryption tests, static analysis, race detector, and builds without starting containers or contacting Spotify.

For the PostgreSQL integration test, point `TEST_DATABASE_URL` at a development/test database where the role can create schemas, then run:

```sh
go test -race -count=1 -run TestPostgresConnectionStore -v ./internal/spotify
```

The test creates an isolated schema and removes it afterward. It checks repeatable migrations, encrypted persistence, session rotation/expiry, transaction rollback, and concurrent updates. Live Spotify authorization still requires your configured app and consent.

## Code reading order

1. `internal/config/spotify.go`: optional configuration and redirect/key validation.
2. `internal/spotify/auth.go`: browser flow, state, sessions, and connection endpoints.
3. `internal/spotify/client.go`: provider requests, PKCE exchange, and refresh response handling.
4. `internal/spotify/store.go`: encryption and transactional persistence.
5. `internal/platform/migrations.go`: embedded, checksummed application migrations.

The code comments explain the security and failure-handling decisions at each step. Provider behavior follows Spotify's [PKCE guide](https://developer.spotify.com/documentation/web-api/tutorials/code-pkce-flow), [refresh guide](https://developer.spotify.com/documentation/web-api/tutorials/refreshing-tokens), and [profile reference](https://developer.spotify.com/documentation/web-api/reference/get-current-users-profile).
