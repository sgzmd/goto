# Architectural Invariants

- Architecture: Minimal Go application using standard library (`net/http`, `html/template`, `embed.FS`) where practical.
- Persistence: All storage access goes through the storage interface. SQLite is the only production backend; no SQL or database concepts escape the adapter.
- Authentication: Generic OIDC authorization-code flow with PKCE, state/nonce validation, and stateless encrypted session cookies. No provider-specific auth code; no local user database.
- Management UI: Server-rendered HTML without frontend frameworks, JS build chains, or CSS frameworks.
- Verification: Every behavioural change requires automated tests. Docker verification must use remote context `max` (`docker --context max ...`).
- Scope: No speculative features, caching layers, analytics, or enterprise abstractions.
