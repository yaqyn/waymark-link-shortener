# API reference

Creation, listing, stats and updates require a session cookie or Basic Auth. Responses include `X-Request-ID`. Creation supports JSON and the original `url` form field; JSON is recommended.

```bash
curl -u frodo:ofTheNineFingers \
  -H 'Content-Type: application/json' \
  --data '{"url":"https://example.com/guide","code":"my-guide","title":"Reading list"}' \
  http://localhost:8899/api/shorten

curl -I http://localhost:8899/my-guide
curl -u frodo:ofTheNineFingers 'http://localhost:8899/api/urls?page=1&q=guide'
```

| Route | Purpose |
| --- | --- |
| `GET /` | Browser workspace |
| `POST /api/login` | JSON username/password or Basic Auth; create session |
| `POST /api/logout` | Invalidate the current browser session |
| `GET /api/session` | Current account |
| `POST /api/shorten` | Create; JSON response contains the complete link record |
| `GET /api/urls?page=1&q=...` | Search; optional `disabled=true` or `disabled=false` filters the saved disabled flag before pagination (not expiration); 20 links/page by default, optional `page_size=1..100`, newest first; returns `links`, `page`, `pages`, `total`, `page_size` |
| `PATCH /api/urls/{code}` | Update `url`, `title`, `expires_at`, `disabled`; include the last known `revision` |
| `GET /api/stats` | Persistent total links, active links and redirects |
| `GET /{code}` | Redirect or return 404/410 |
| `GET /healthz` | Process health |
| `GET /metrics` | Prometheus; authenticated in production |
| `GET /debug/pprof/` | Authenticated profiles, development only |
| `POST /admin/shutdown` | Authenticated development-only shutdown |

Custom codes use 4–64 ASCII letters, digits, underscores or hyphens. Reserved service paths are rejected. Generated codes have 10 characters. Codes are case-sensitive; uppercase legacy files can also be looked up by a lowercase spelling.

Set `expires_at` to a future RFC3339 timestamp, or `null` for no expiration. Disabled/expired links return **410 Gone**. Re-enabling is an edit; expired links also need their expiration cleared or moved forward. Stale revision updates return **409 Conflict**. Disabling preserves the record and its analytics; there is no hard-delete endpoint.


## Request bodies and failures

JSON creation accepts `url`, `code`, `title`, `expires_at` and `disabled`. Editing uses `url`, `title`, `expires_at`, `disabled` and a nonzero `revision`; send the complete editable state. The shortcode remains unchanged. JSON mutation bodies are limited to 16 KiB, reject unknown fields and must contain exactly one object.

```json
{
  "url": "https://example.com/updated-guide",
  "title": "Updated guide",
  "expires_at": null,
  "disabled": false,
  "revision": 1
}
```

Send this to `PATCH /api/urls/api-guide` with the revision returned by your last read. Successful updates return the updated JSON record and increment its edit revision. Redirect visits update counters without changing that revision.

| Status | Meaning |
| --- | --- |
| 400 | Invalid body, destination, code, title, expiry, page, page size or disabled filter |
| 401 | Authentication required or invalid credentials |
| 403 | Cross-site mutation rejected |
| 404 | Link or route not found |
| 409 | Code collision or stale edit |
| 410 | Link disabled, expired or unsafe legacy destination |
| 429 | Authentication attempt limit exceeded |
| 500 | Internal failure; full diagnostic details remain in logs |

Error bodies are text with a trailing newline. 401, 403 and 500 use generic HTTP status text; other errors retain their message. Use `X-Request-ID` to match a response to its request log. Login with JSON `{ "username": "...", "password": "..." }` or Basic Auth; a successful login sets a session cookie. Cookie-based mutations are restricted to the same origin.
