# Migrating from Linko to Waymark

The rebrand changes operational names, not your stored link records. Stop the old process before starting Waymark against the same data directory. Make a backup first; do not run both against that directory.

## Names to update

| Previous name | Waymark name |
| --- | --- |
| `linko` binary | `waymark` |
| `LINKO_BIND_ADDR` | `WAYMARK_BIND_ADDR` |
| `LINKO_USERNAME` | `WAYMARK_USERNAME` |
| `LINKO_PASSWORD_HASH` | `WAYMARK_PASSWORD_HASH` |
| `LINKO_PASSWORD_HASH_FILE` | `WAYMARK_PASSWORD_HASH_FILE` |
| `LINKO_LOG_FILE` | `WAYMARK_LOG_FILE` |
| `LINKO_TRACING` | `WAYMARK_TRACING` |
| `LINKO_BASE_URL` in the traffic helper | `WAYMARK_BASE_URL` |
| `LINKO_TEST_USERNAME` / `LINKO_TEST_PASSWORD` | `WAYMARK_TEST_USERNAME` / `WAYMARK_TEST_PASSWORD` |
| `linko_session` cookie | `waymark_session` |
| `OTEL_SERVICE_NAME=linko` | `OTEL_SERVICE_NAME=waymark` |
| Prometheus `job="linko"` | `job="waymark"` |
| `boot.dev/linko` Go module/scope | `github.com/yaqyn/waymark-link-shortener` |

Old `LINKO_*` environment variables are no longer read. `ENV`, `OTEL_EXPORTER_*`, flags, the port, API paths and shortcode syntax are unchanged. Keep the same `-data` path or volume to keep your links. JSON and plain-URL legacy records remain supported.

## Restart and verify

```bash
make build
WAYMARK_USERNAME=owner WAYMARK_PASSWORD_HASH_FILE=./password.hash \
  WAYMARK_LOG_FILE=waymark.access.log ./waymark -data ./data
```

Sign in again: sessions are memory-only and the cookie name changed. Verify `/healthz`, a known short link, and the workspace list. Existing access logs are historical records; the new log path starts a separate file if you choose one. Rebuild integrations that use the old module path.

Restart Prometheus after updating `prometheus.yml`. Queries filtered by `job="linko"` must use `job="waymark"`; metric names and labels stay the same. Jaeger keeps old traces under the old service name. Grafana dashboards and alert rules created manually are not provisioned by this repo; update service/job filters there if used.

## Repository and volume names

The working directory and Git remote do not determine the app name. Waymark is published at [yaqyn/waymark-link-shortener](https://github.com/yaqyn/waymark-link-shortener). The original Boot.dev remote is retained as `upstream`; `origin` points to Waymark.

If you rename the local directory, Docker Compose may derive a different project name and create fresh named volumes. Keep the existing Compose project name with `docker compose -p <existing-project-name> ...` when retaining its monitoring state. Inspect `docker compose ls` first. The standalone Docker example uses a new `waymark_data` volume; mount your existing data volume instead when migrating.
