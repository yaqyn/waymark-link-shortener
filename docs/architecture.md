# Architecture

Waymark serves one private workspace with public redirects. Its storage boundary is a single Go process writing to a local data directory. Monitoring components do not participate in creating or following links.

## Request paths

Management requests pass through origin checks and authentication before the handlers. Browser login creates an HttpOnly cookie backed by an in-memory session. Basic Auth is available to API clients. Both resolve to the same configured account and permissions.

A redirect validates the stored destination, checks availability state and persists a visit before returning HTTP 302. HEAD checks do not increment counters. Validation never makes a network request. Disabled and expired links return 410; missing links return 404.

Request middleware creates or propagates an ID, applies security headers, records status and body sizes, measures elapsed time and captures response errors. Metrics use matched route patterns to bound label cardinality. Tracing wraps the root handler only when export is enabled; child spans describe handlers and destination validation.

## Persistent records

Each shortcode names a file containing a JSON record with destination, title, timestamps, expiry, enabled state, clicks and revision. Writes use a synced temporary file, atomic publication and a directory sync. A mutex protects reads and updates in this process. Creation detects collisions; edits require the last known revision.

Visits update counters without advancing the edit revision. Plain-URL legacy files remain readable and upgrade when visited or edited. Invalid filenames, symlinks and oversized records are rejected. Listing collects failures into a joined error rather than silently returning partial results.

Atomic files protect publication from partial writes; they do not provide cross-process transactions. Per-click disk sync and whole-directory listing are intentional limits of this backend.

## Repository map

| Path | Responsibility |
| --- | --- |
| `main.go` | Runtime configuration, logger, build metadata and shutdown |
| `server.go` | Routes, shared HTTP wrappers, errors and request logs |
| `auth.go` | Configured credentials, sessions, login limits and origin checks |
| `handlers.go` | Embedded interface, create/list/edit/stats and redirects |
| `destination.go` | URL syntax and credential policy |
| `metrics.go` | Request counter and duration histogram |
| `tracing.go` | Optional OpenTelemetry exporter and tracer |
| `internal/store/` | Atomic file storage and record lifecycle |
| `internal/apperr/` | Structured error attributes carried through wrapping |
| `internal/build/` | Git SHA and build-time strings injected at link time |
| `web/` | Browser interface, shared styles and interaction code |
| `docker-compose.yaml` / `prometheus.yml` | Local monitoring services and scrape jobs |

## Boundaries and tradeoffs

The destination is supplied by a trusted operator. Rejecting embedded credentials prevents accidental credential disclosure; accepting HTTP/HTTPS syntax does not classify destination content. Public redirects can still point to harmful sites.

Session state is ephemeral; link state is durable. Restarting intentionally ends sessions. Search scans all records before pagination, so the API limits rendered results rather than database work. The single-account model has no tenant isolation. These choices make the system inspectable and easy to run, but do not support a public, multi-user hosting service without further work.

Product rules live in [PRODUCT.md](../PRODUCT.md), interaction behavior in [UX-CONTRACT.md](../UX-CONTRACT.md), and visual values in [DESIGN.md](../DESIGN.md). Change the corresponding contract when making a deliberate system decision.
