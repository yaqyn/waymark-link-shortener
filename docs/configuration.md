# Configuration

Waymark is a **single-account workspace**. Every authenticated client can manage every link; there is no public registration or per-user isolation.

Generate a bcrypt hash without putting a plaintext password in a command argument or shell history:

```bash
# Bash: enter a password of 12–72 bytes when prompted.
umask 077
read -rs -p 'New Waymark password: ' waymark_password; printf '\n'
printf '%s' "$waymark_password" | go run . -hash-password > password.hash
unset waymark_password
chmod 600 password.hash

WAYMARK_USERNAME=owner WAYMARK_PASSWORD_HASH_FILE=./password.hash go run .
```

`.env.example` documents settings; Waymark does not auto-load `.env` files. You may use your deployment's secret-file mechanism for `WAYMARK_PASSWORD_HASH_FILE`.

| Setting | Behavior |
| --- | --- |
| `ENV=production` | Require configured password hash, use JSON stderr logs, secure cookies, hide profiling/shutdown routes and authenticate metrics |
| `WAYMARK_BIND_ADDR` | Bind interface; defaults to `127.0.0.1`. Use `0.0.0.0` for container/controlled Docker-host access |
| `WAYMARK_USERNAME` | Workspace account name; defaults to `frodo` |
| `WAYMARK_PASSWORD_HASH_FILE` | File containing the bcrypt hash; takes precedence over the hash environment variable |
| `WAYMARK_PASSWORD_HASH` | Alternative bcrypt hash; quote it correctly so `$` characters are preserved |
| `WAYMARK_LOG_FILE` | Optional rotating JSON log file; stderr logging remains enabled |
| `WAYMARK_TRACING=true` | Enable tracing without requiring a service-name setting |
| `OTEL_SERVICE_NAME=waymark` | Enable the tracing exporter and name the service |
| `OTEL_EXPORTER_OTLP_TRACES_INSECURE=true` | Use plaintext gRPC for a local collector; do not use this for an untrusted network |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Optional collector endpoint configured by OpenTelemetry |

Browser login uses a 12-hour HttpOnly, SameSite=Strict cookie. Sessions are kept in memory and end on restart. API clients may use HTTP Basic Auth over HTTPS. Authentication attempts are limited to 30 per minute across the single-account workspace; successful session requests avoid repeated bcrypt work. Cross-site mutations are rejected.


The app-specific environment prefix changed during the rebrand; see [migration](migration.md). Read the [operations guide](operations.md) before enabling production mode or making the backend reachable beyond localhost.
