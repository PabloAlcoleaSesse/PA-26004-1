# Contributing

The Go backend foundation is in place. Issues describing use cases, integration research, and proposed designs are welcome.

Before starting a substantial implementation, open an issue describing the problem, expected behavior, and proposed approach so the scope can be discussed.

For pull requests:

- Keep changes focused and explain what they solve.
- Include relevant verification steps and any known limitations.
- Update documentation when behavior or setup changes.
- Use synthetic examples instead of personal listening history or account data.
- Never commit credentials, access tokens, or local environment files.

Use Go's standard formatting (`make fmt`), keep packages focused, and pass contexts through database and network operations. Keep provider-specific behavior out of shared transfer logic.

Run `make check` before submitting changes. For database changes, use `TEST_DATABASE_URL` to run the isolated PostgreSQL integration tests described in the Spotify guide. Container smoke tests are optional and run only when explicitly requested; CI exposes a manual option. Add tests for behavior and failure cases introduced by your change. Never use real Spotify credentials in fixtures or logs.
