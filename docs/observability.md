# Follow a request through Waymark

The shortener gives users a task: create a link and share it. The engineering work makes that task observable. A failed response carries a request ID; logs preserve the diagnostic error; metrics expose patterns; traces separate the operations inside a request. Profiling answers a different question: where CPU time or retained memory is spent.

This guide is a source tour and a local diagnostic workflow. It does not claim a hosted monitoring service or measured production reliability.

## Run the diagnostic stack

Follow the [local monitoring setup](operations.md#local-monitoring). It starts Prometheus, Grafana, Node Exporter and Jaeger alongside the separately running app. Grafana provisions **Waymark App Metrics** in its **Waymark** folder and a **Waymark Prometheus** data source. Open http://localhost:3000 with the local stack's `admin` / `admin` credentials.

The dashboard contains request rate by status, response counts over the selected range, p95 duration by route, and server errors per minute. It uses a five-minute window and refreshes every five seconds. Empty panels can mean no traffic or too few scrapes for a rate; create links and follow redirects, then check the Prometheus target before diagnosing the app.

Dashboard definitions live in [monitoring/grafana](../monitoring/grafana). Edit those files to retain changes; the provisioned dashboard is read-only in Grafana. Existing manually created dashboards remain in the named volume.

## Logs: one request, useful context

[requestLogger](../server.go) measures duration, request bytes, response bytes and status after the handler returns. It reads the authenticated account and response error from the shared LogContext. [httpError](../server.go) records the full diagnostic error there while returning generic text for 401, 403 and 500 responses.

[initializeLogger and replaceAttr](../main.go) configure both output handlers and transform error values. Console logs use tint during development and JSON in production; an optional rotating JSON file receives INFO and above. Error fields contain a message, extracted attributes and a stack trace when available. Joined errors become numbered groups. The store attaches file-path context to returned errors rather than logging the same failure again.

An illustrative entry, abbreviated rather than copied from user data:

```json
{"level":"ERROR","msg":"Served request","method":"GET","path":"/sample-code","request_id":"example-request","response_status":500,"error":{"message":"permission denied","path":"data/sample-code"}}
```

Each entry also carries Git SHA, build time, environment and hostname. Build through `make build` to populate build metadata. With tracing enabled, request logs add `trace_id` and `span_id`. An inbound `X-Request-ID` is retained only if it passes the identifier policy; otherwise the app generates one and sends it back.

Sensitive-key redaction and URL-password filtering run in the logging handlers, while request logging masks the last IPv4 octet. These filters are safeguards, not proof that arbitrary text is secret-free. IPv6 addresses currently remain unchanged. Do not put secrets in URLs or error messages.

## Metrics: patterns without per-link series

[metricsMiddleware](../metrics.go) records `http_requests_total{method,path,status}` and `http_request_duration_seconds{method,path}`. The path label uses the matched route pattern, such as `/{shortCode}`, instead of every individual code. Unsupported methods collapse to `OTHER`. Health and scrape requests are excluded from these app metrics.

Counters need a rate or increase to answer a time-window question. Histogram buckets need aggregation before calculating a percentile. The provisioned dashboard demonstrates both:

```promql
sum(rate(http_requests_total{job="waymark"}[5m])) by (status)

histogram_quantile(0.95,
  sum(rate(http_request_duration_seconds_bucket{job="waymark"}[5m])) by (le, path)
)
```

For a local alert exercise, use server failures rather than bad passwords:

```promql
sum(rate(http_requests_total{job="waymark",status=~"5.."}[5m])) * 60
```

A threshold above 3 with a 20-second pending period reproduces the course's final behavior. It is an educational threshold, not a production recommendation. Alert rules and notification routing are not provisioned here; choose them for actual traffic, failure budgets and operator responsibilities. Node Exporter supplies host memory, CPU and filesystem metrics separately.

## Traces: timings inside the request

[initTracing](../tracing.go) creates the OTLP/gRPC exporter, batches spans, sets the provider and installs W3C trace-context propagation. Shutdown drains the exporter with a bounded context. The tracing middleware creates the request span; handlers, password validation and destination validation create child spans using the incoming context.

Open Jaeger at http://localhost:16686, select service **waymark**, then find a login or shorten request. A returned HTTP error is recorded on the active span, and a server failure sets its status to Error. Use a log's trace ID to locate its trace when available.

Tracing is optional. Omitting the service name and `WAYMARK_TRACING` disables export; the link workflow still works. Plaintext OTLP is intended for the local stack. Protect collectors and use appropriate transport security outside that environment.

## Profiling: resource costs

Authenticated pprof routes are available only outside production. Collect from a running binary using the public development credentials only in local development:

```bash
make build
# Run ./waymark in another terminal, then:
curl -u frodo:ofTheNineFingers \
  'http://localhost:8899/debug/pprof/profile?seconds=30' --output cpu.prof

go tool pprof -top waymark cpu.prof
```

Generate redirect traffic during collection with `./spamredirect.sh 200`. The helper creates a new test link rather than overwriting an existing record. Heap captures are available under `/debug/pprof/heap`; use Graphviz only when exporting graphical profiles. Profile files can expose internal details and are ignored by Git.

The original course taught diagnosis through deliberate CPU and memory defects. The current redirect path has removed those defects; historical captures are not performance evidence for this version.

## Engineering decisions worth inspecting

| Decision | Consequence | Source |
| --- | --- | --- |
| Log response errors at the request boundary | One contextual response event instead of repeated low-level failures | `server.go`, `internal/apperr/` |
| Keep client error text generic where needed | Diagnostic detail stays in logs rather than leaking in 500 responses | `httpError` |
| Use route patterns for metrics | More short codes do not create more path-label series | `metrics.go`, bounded-label test |
| Carry context into child spans | Handler timings belong to the request trace | `tracing.go`, `handlers.go`, `auth.go` |
| Make telemetry optional | Running the shortener does not require a collector | `initTracing` |
| Protect profiling and hide it in production | Runtime internals are not exposed as a public production route | `server.go`, production-mode tests |

These choices are inspectable behavior, not a claim that logging alone guarantees reliability. [Verification](../README.md#-verification) covers local checks; [operations](operations.md) explains storage, authentication, backup and deployment limits.
