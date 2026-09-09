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

Run `make check` before submitting changes. For database, worker, or container changes, also start the Compose stack and run the probe described in the README. Add tests for behavior and failure cases introduced by your change. CI repeats the checks and the container smoke test.
