# Waymark identity

Waymark is a private workspace for shareable links: create a shorter address, retain control of its destination, and keep a durable record of redirects. The primary reader is an operator trying the software locally. Developers evaluating the implementation are served by the architecture, API and operations guides. These are task assumptions drawn from the project brief, not external user research.

## Name and expression

A waymark points a traveller along a route. Here it points a shared address toward its destination. The name is an editorial choice; no trademark clearance, exclusive ownership or domain availability is claimed.

The route-marker post is the signature. Use it in the app wordmark, favicon and cover. Ink-blue surfaces and pale-blue accents keep the interface, README and diagrams in one family. The sign-in introduction uses a restrained serif; controls and data use system sans, with monospace for addresses. Exact colors and runtime mappings live in [DESIGN.md](DESIGN.md); artwork conventions live in [readme-assets/STYLE.md](readme-assets/STYLE.md).

## Writing

Lead with the action and consequence. Use “Create short link,” “Save changes,” “Disable this link” and “Copy link” consistently. Describe failure with enough detail to recover. State limits beside affected instructions. Do not imply that redirect counts are unique visitors or that syntactic URL validation guarantees a safe destination.

The project category is **private link workspace**. Use Waymark for the product, `waymark` for the binary and local telemetry service, and `WAYMARK_*` for app-specific settings. Historical Linko references belong in migration notes and provenance.

## Ownership and provenance

Maintained by Abdulrahman M. Yaqyn. The project began in Boot.dev's Logging and Telemetry course and retains its MIT notice. Independent extensions should be described by their implemented behavior. See [README.md](README.md) for the reader-facing introduction and [docs/migration.md](docs/migration.md) for changed operational names.
