# goto

Goto is an internal short-link redirect service with an OIDC-authenticated management interface and SQLite persistence.

## Testing

```sh
go test ./...
go test -race ./...
```

## Running Locally

Set required environment variables and start the server:

```sh
export BASE_URL="http://localhost:8080"
export OIDC_ISSUER_URL="https://accounts.google.com" # or https://<tenant>.okta.com
export OIDC_CLIENT_ID="<client-id>"
export OIDC_CLIENT_SECRET="<client-secret>"
export SESSION_SECRET="<random-string-at-least-32-chars>"
export DB_PATH="./goto.db" # optional, defaults to /data/goto.db

go run ./cmd/goto
```

## OIDC Configuration

Register the OAuth 2.0 / OIDC redirect URI with your identity provider (Google, Okta, or any OIDC-compliant IdP):

```
${BASE_URL}/auth/callback
```

Required scopes: `openid`, `profile`, `email`.

## Docker (Context `max`)

Build:

```sh
docker --context max build -t goto:latest .
```

Run:

```sh
docker --context max run -d \
  --name goto \
  -p 8080:8080 \
  -v goto-data:/data \
  -e BASE_URL="https://go.example.com" \
  -e OIDC_ISSUER_URL="https://auth.example.com" \
  -e OIDC_CLIENT_ID="<client-id>" \
  -e OIDC_CLIENT_SECRET="<client-secret>" \
  -e SESSION_SECRET="<min-32-char-secret>" \
  -e SECURE_COOKIES=true \
  goto:latest
```

SQLite database file is persisted at `/data/goto.db`.
Container runs as non-root user `10001:10001`.
Health check endpoint: `GET /healthz`.
