# Waymark product contract

Waymark is a private workspace for a single operator to create and manage public HTTP redirects. Creation, listing, analytics and edits require authentication; following a shared short link does not. This access model follows the owner's explicit selection on 2026-10-12.

- One configured account manages the shared workspace. There is no public registration, billing or multi-tenant ownership model.
- Destinations must be complete HTTP/HTTPS URLs without embedded credentials. Waymark never fetches them: creation must not become an SSRF proxy, and redirects must not depend on an upstream availability check.
- A link may have a title, custom code and expiration. Disabling is reversible and returns HTTP 410 when followed. There is no irreversible delete operation.
- Click totals and link metadata survive restarts. Requests must not retain an unbounded in-memory history.
- Updates require the last known revision to prevent stale edits from overwriting newer edits.
- File persistence is for one process per data directory. Multiple replicas need a shared transactional database before deployment.
- Development has a documented demo account. Production requires configured credentials and HTTPS for secure session cookies.
- Telemetry is optional; the shortener works without Docker or an external collector.
