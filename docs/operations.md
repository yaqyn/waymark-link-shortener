# Operations

## Local monitoring

Requires Docker Compose and the Go toolchain from the README. The command below makes the host app reachable by Docker on all interfaces; use it on a controlled development network. The Compose interfaces remain bound to localhost. Stop any already-running app before starting this one.

```bash
docker compose up -d
WAYMARK_BIND_ADDR=0.0.0.0 OTEL_SERVICE_NAME=waymark \
  OTEL_EXPORTER_OTLP_TRACES_INSECURE=true WAYMARK_LOG_FILE=waymark.access.log go run .
```

| Tool | Local interface |
| --- | --- |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 (`admin` / `admin` for this local stack) |
| Jaeger | http://localhost:16686 |
| Node Exporter | http://localhost:9100/metrics |

Grafana provisions the **Waymark App Metrics** dashboard and **Waymark Prometheus** data source from `monitoring/grafana/`. See the [observability walkthrough](observability.md) for the panels and a request-diagnosis source tour.

The monitoring command binds Waymark on all interfaces so Docker can scrape the host. Keep host ingress controlled; normal `go run .` binds loopback only. Compose binds monitoring ports to localhost. Its Node Exporter mount is intended for Linux development. The stack is a local diagnostic setup, not a public monitoring deployment. Grafana state persists in `grafana_data` and metric history in `prometheus_data`; avoid `docker compose down -v` if you want to keep it.

Request counters and duration histograms use **route patterns** such as `/{shortCode}`, so every new code does not create a new metric series. Health checks and metrics scrapes are excluded from application request counts, successful access logs and traces. Prometheus scrapes every five seconds in this local stack.

Useful PromQL:

```promql
# Requests/second, grouped by status.
sum(rate(http_requests_total[5m])) by (status)

# 95th-percentile response duration, by route.
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, path))

# Server errors/minute.
sum(rate(http_requests_total{status=~"5.."}[5m])) * 60
```

Jaeger traces include handler and destination-validation spans. Request logs include trace IDs when tracing is active. Destination validation checks syntax; no network verification is performed. Structured logs redact sensitive keys, credential-bearing URLs and IPv4 final octets. Do not put secrets in URL paths or queries: a key filter cannot make arbitrary user-supplied text safe.

The original CPU/memory capture scripts remain available for inspection. Course profiles describe the old deliberate bottlenecks; after this enhancement their original performance defects are removed.

## Storage and deployment

- **One process per data directory.** Mutexes coordinate concurrent requests in one process; atomic publication prevents partial records. Use a transactional database before running multiple writers/replicas. Directory scans and per-click synced writes suit a modest private workspace, not high-volume analytics.
- **Back up `data/`.** Link metadata and counters live there. Session state is intentionally ephemeral. Existing plain-URL course records remain readable and upgrade to JSON when visited or edited.
- **Use HTTPS in production.** Run behind a reverse proxy that preserves the original Host header. Secure cookies intentionally do not work over plain HTTP. Set `ENV=production`, supply a private hash file, restrict backend ingress, and keep collectors private.
- **No public sign-up or abuse-management service.** Public redirect links can still be used to share harmful destinations; only grant workspace access to trusted operators. Add domain policy and reporting if you plan a public service.
- **Service timeouts and limits:** 5-second header timeout, 15-second read timeout, 16 KiB headers and mutation bodies, 4096-character URLs, 100-character titles. Shutdown drains requests and tracing within bounded deadlines.
- **Legacy credential URLs:** old course records are preserved, but credential-bearing destinations are blocked until edited. The UI hides their credentials and marks them as needing an update.
- **Generated artifacts stay local:** credentials, logs, profiles, data and the binary are ignored by Git.

A multi-stage `Dockerfile` builds a non-root container with persistent storage at `/data`:

```bash
docker build -t waymark .
docker run --rm -p 127.0.0.1:8899:8899 -v waymark_data:/data waymark
```

This command uses development mode. Production needs the HTTPS reverse proxy and configured credentials described above.


## Troubleshooting

| Symptom | Check and remedy |
| --- | --- |
| Server exits with an address-in-use error | Stop the other listener or set `-port`; update client and scrape URLs too |
| Production refuses to start | Set a valid bcrypt hash through `WAYMARK_PASSWORD_HASH_FILE`; supported cost is 10–14 |
| Login succeeds but the production session is missing over HTTP | Serve the original host through HTTPS; production cookies are Secure |
| API returns 429 during authentication | The shared account permits 30 attempts/minute, including Basic-authenticated requests; reuse a browser session or wait |
| Edit returns 409 | Refresh to load the current revision and reapply the intended change |
| Redirect returns 410 | Check disabled state, expiration or an unsafe legacy destination; edit the record in the workspace |
| Links or stats return 500 | Inspect the structured error for a corrupt/unreadable record; back up before repairing files |
| Prometheus target is down | Bind the host app explicitly for Docker access and check port 8899; production metrics also require authentication |

## Backup and recovery

Stop Waymark or take a filesystem snapshot before copying its data directory. Keep backup permissions private. Restore into a separate directory first, start one instance with `-data` pointing there and verify known destinations, counters and the workspace list. The app does not provide automatic backups or multi-file snapshot transactions. Restarting clears sessions; it does not clear stored links.

Press Ctrl-C to stop the app. Stop the local monitoring stack with `docker compose down`; leave named volumes intact when retaining dashboards and metric history. Production metrics require Basic Auth, so configure scrape credentials through private deployment configuration rather than committing them to `prometheus.yml`. The checked-in scrape job is for local development.
