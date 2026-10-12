# Contributing to Waymark

Start by describing the concrete behavior, trigger and expected result. Read [PRODUCT.md](PRODUCT.md) for the access and storage boundaries, [DESIGN.md](DESIGN.md) for visual decisions, and [UX-CONTRACT.md](UX-CONTRACT.md) for interaction behavior. The [architecture guide](docs/architecture.md) maps the implementation.

## Local checks

Use Go 1.27 or later, then run from the project root:

```bash
go mod download
go test -race ./...
go vet ./...
go build -o waymark .
```

For UI changes, check sign-in, creation, editing, search, error recovery, keyboard focus and a narrow viewport in a browser. Preserve form values on failures and keep sensitive input out of logs. Validate changed documentation links and SVGs, and run `git diff --check`.

## Scope and review

Keep changes focused and explain why they belong in this project. Add behavior tests when changing persistence, authentication, redirects or concurrency. Do not introduce tests that simply repeat a reversible prose or visual edit.

Retain old record compatibility. State migration steps for operational changes. Multiple writers, multi-account ownership, irreversible deletion or external destination fetching require a documented revision of the product contract; do not introduce them as incidental refactors.

Use example URLs and disposable local data. Never submit password files, access logs, profiles, session cookies or live link records. Preserve the upstream license and legitimate attribution. Describe what was checked and any remaining limit; a successful local test is not a deployment claim.

Open issues and pull requests in [yaqyn/waymark-link-shortener](https://github.com/yaqyn/waymark-link-shortener). Preserve the Boot.dev upstream as provenance; do not send Waymark-specific publication changes to that upstream.
