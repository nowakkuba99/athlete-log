# Security Policy

## Scope

Athlete Log is designed as a **self-hosted, single-user application**. The primary threat model is an attacker who gains access to the network the instance is running on — not a multi-tenant SaaS scenario.

Security issues in the following areas are in scope:

- Authentication bypass or privilege escalation
- SQL injection or other data-layer vulnerabilities
- XSS or CSRF in the web UI
- Sensitive data exposure in API responses
- Unsafe handling of uploaded files

Out of scope: denial-of-service, issues that require physical access to the host, or vulnerabilities in third-party dependencies (report those upstream).

## Supported versions

Only the latest commit on `main` is actively supported. There are no versioned release branches at this time.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Use [GitHub's private vulnerability reporting](https://github.com/nowakkuba99/athlete-log/security/advisories/new) to submit a report confidentially.

Include:

1. A description of the vulnerability and its impact
2. Steps to reproduce (minimal reproduction is ideal)
3. Any proof-of-concept code or screenshots

You can expect an acknowledgement within 48 hours and a fix or mitigation plan within 7 days for confirmed issues.

## Security configuration

Before exposing Athlete Log beyond localhost:

1. **Change `POSTGRES_PASSWORD`** in `docker-compose.yml` from the default `athlete` to a strong password.
2. **Enable password protection** in Settings → Password protection — this gates the entire UI and API behind a bcrypt-hashed password.
3. Run behind a reverse proxy (nginx, Caddy) with TLS if accessible over the internet.

If you forget the password, restart with `APP_AUTH_RESET=true` to clear it (see the [Security docs](https://athlete-log.app/docs/security/#forgotten-password)).

See [Setup → Security](https://athlete-log.app/docs/setup/#security) for full instructions.
