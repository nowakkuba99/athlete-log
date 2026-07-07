# Security

Athlete Log is designed as a **self-hosted, single-user application**. The default configuration is safe for localhost use out of the box. Before exposing it on a home network or the internet, follow the steps below.

---

## Changing the database password

The default `POSTGRES_PASSWORD` in `docker-compose.yml` is `athlete`. This is fine on localhost but must be changed before any network exposure.

Edit `docker-compose.yml` before the first run:

```yaml
environment:
  POSTGRES_PASSWORD: your-strong-password-here
```

Update the matching `DATABASE_URL` in the same file:

```yaml
DATABASE_URL: postgres://athlete:your-strong-password-here@db:5432/athlete_log?sslmode=disable
```

If you've already started the stack with the default password, stop it, delete the Postgres volume (`docker compose down -v`), update the compose file, and run again. **There is no safe in-place password change for Postgres without recreating the volume.**

---

## Password protection

By default, no login is required — anyone who can reach the app URL can see your data. To gate access:

1. Open **Settings → Password protection**
2. Toggle **Enable password protection** on
3. Enter and confirm a password → **Save**

The password is hashed with [bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt) at default cost and stored in the database — never in plaintext. The browser stores a session token in `sessionStorage` (cleared when the tab closes). Re-opening the app in a new browser or tab will prompt for the password again.

To disable: open Settings, toggle off, Save. You must already be authenticated to disable it.

### Forgotten password

If you lose the password, restart the container with `APP_AUTH_RESET=true`:

```sh
docker compose stop app
APP_AUTH_RESET=true docker compose up app
```

On startup the server clears `auth_enabled` and `auth_password_hash` from the database and logs `APP_AUTH_RESET=true: auth disabled and password cleared`. The app is then accessible without a password. Go to Settings to set a new one.

Remove `APP_AUTH_RESET=true` from your environment before the next restart so auth stays enabled.

---

## Running behind a reverse proxy

If you're exposing Athlete Log over the internet, put it behind a reverse proxy with TLS:

=== "Caddy"

    ```caddy
    vault.example.com {
        reverse_proxy localhost:8080
    }
    ```

    Caddy handles TLS certificates automatically via Let's Encrypt.

=== "nginx"

    ```nginx
    server {
        listen 443 ssl;
        server_name vault.example.com;
        ssl_certificate     /etc/letsencrypt/live/vault.example.com/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/vault.example.com/privkey.pem;

        location / {
            proxy_pass http://localhost:8080;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
        }
    }
    ```

Set `BASE_URL=https://vault.example.com` in `docker-compose.yml` — this is required for Strava OAuth redirects to work correctly when not on localhost.

---

## Reporting a security issue

See the repository `SECURITY.md` for the vulnerability disclosure process.
