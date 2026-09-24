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
export OIDC_ISSUER_URL="https://accounts.google.com"
export OIDC_CLIENT_ID="<client-id>"
export OIDC_CLIENT_SECRET="<client-secret>"
export SESSION_SECRET="<random-string-at-least-32-chars>"
export DB_PATH="./goto.db" # optional, defaults to /data/goto.db

go run ./cmd/goto
```

## OIDC Provider Configuration

Register the callback URI in your IdP console:

```
${BASE_URL}/auth/callback
```

Required scopes: `openid`, `profile`, `email`.

### 1. Local Mock IdP (Free, Offline, No Account Needed)

Run the included local OIDC test server in a separate terminal:

```sh
PORT=8081 PUBLIC_URL="http://localhost:8081" go run ./test/cmd/mockoidc
```

Then configure `goto`:

```sh
export OIDC_ISSUER_URL="http://localhost:8081"
export OIDC_CLIENT_ID="test-client-id"
export OIDC_CLIENT_SECRET="test-client-secret"
```

### 2. Google (Free Testing Mode)

1. In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), configure the OAuth consent screen (User Type: External, Publishing status: Testing).
2. Add your Google account under **Test users**.
3. Create credentials: **OAuth client ID** > **Web application**.
4. Add Authorized redirect URI: `http://localhost:8080/auth/callback` (or your `${BASE_URL}/auth/callback`).
5. Configure `goto`:

```sh
export OIDC_ISSUER_URL="https://accounts.google.com"
export OIDC_CLIENT_ID="<client-id>.apps.googleusercontent.com"
export OIDC_CLIENT_SECRET="<client-secret>"
```

### 3. Okta (Free Developer Account)

1. Sign up for a free developer tenant at [developer.okta.com](https://developer.okta.com/).
2. Navigate to **Applications** > **Create App Integration** > **OIDC - OpenID Connect** > **Web Application**.
3. Set **Sign-in redirect URIs** to `${BASE_URL}/auth/callback`.
4. Configure assignments (e.g. "Allow everyone in your organization to access").
5. Configure `goto`:

```sh
export OIDC_ISSUER_URL="https://<your-okta-domain>.okta.com" # or https://<domain>.okta.com/oauth2/default
export OIDC_CLIENT_ID="<client-id>"
export OIDC_CLIENT_SECRET="<client-secret>"
```

### 4. Auth0 (Free Tier)

1. Create a free tenant at [auth0.com](https://auth0.com/).
2. Navigate to **Applications** > **Create Application** > **Regular Web Applications**.
3. In **Settings**, add to **Allowed Callback URLs**: `${BASE_URL}/auth/callback`.
4. Configure `goto`:

```sh
export OIDC_ISSUER_URL="https://<your-tenant>.<region>.auth0.com/"
export OIDC_CLIENT_ID="<client-id>"
export OIDC_CLIENT_SECRET="<client-secret>"
```

### 5. Microsoft Entra ID / Azure AD (Free Tenant)

1. In the [Azure Portal](https://portal.azure.com/) or Entra admin center, go to **App registrations** > **New registration**.
2. Under **Redirect URI**, select **Web** and enter `${BASE_URL}/auth/callback`.
3. Under **Certificates & secrets**, create a new client secret.
4. Configure `goto`:

```sh
export OIDC_ISSUER_URL="https://login.microsoftonline.com/<tenant-id>/v2.0"
export OIDC_CLIENT_ID="<application-client-id>"
export OIDC_CLIENT_SECRET="<client-secret-value>"
```

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
  -e OIDC_ISSUER_URL="https://accounts.google.com" \
  -e OIDC_CLIENT_ID="<client-id>" \
  -e OIDC_CLIENT_SECRET="<client-secret>" \
  -e SESSION_SECRET="<min-32-char-secret>" \
  -e SECURE_COOKIES=true \
  goto:latest
```

SQLite database file is persisted at `/data/goto.db`.
Container runs as non-root user `10001:10001`.
Health check endpoint: `GET /healthz`.
