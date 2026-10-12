# Waymark interaction contract

## Business context sources

Permissions, link lifecycle, destination validation and revision checks: [PRODUCT.md](PRODUCT.md), implemented by auth.go, handlers.go and internal/store/store.go. No payments or legal acceptance flow.

## Canonical UI Map

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
|---|---|---|---|---|
| Form | web/app.js readInput, fieldError, clearErrors, busy | PRODUCT.md / DESIGN.md | create, edit, login | backend tests + browser flows |
| Date | native datetime-local input | DESIGN.md | optional expiry; platform popup accepted | local-time conversion + browser |
| Scrollbar | web/app.css root / scrollbar rules | DESIGN.md | table overflow, dialog overflow | desktop/narrow inspection |
| Toast | web/app.js notice + #notice | DESIGN.md | success/error; critical errors stay inline | browser live regions |
| CRUD | web/app.js API helpers, refresh, edit dialog | PRODUCT.md | create, edit, reversible disable | revision/expiry/auth tests + browser |

No selectable grid or select/listbox is present. Platform-owned date-popup geometry is accepted.

## Flow ledger

- Login: disable submit → session cookie → workspace, refresh links and focus destination. Wrong credentials stay inline with password focus. No credential persistence in JS storage.
- Create: validate → disable submit → persist → keep workspace, show copyable short link and refresh first page. A failure retains all entered data.
- List: server pagination (20), committed q/page in URL, debounced/cancelled search, immediate clear/Enter. Loading, empty, no-results and retry-through-Refresh states. Old responses cannot overwrite new ones.
- Edit: native app-owned dialog → revision-checked save → close → refresh; stale edits show conflict and preserve entries. Cancel/Escape restores trigger focus. Busy save blocks closing.
- Disable: the edit checkbox describes the consequence; Save applies it. Disabling is reversible by editing again. No hard delete or confirmation claiming irreversibility.
- Session expiry: return to login, preserve creation input and show reauthentication message. Sign out invalidates the cookie server-side and clears workspace results.
- Clipboard: explicit button; failure provides a selectable link and manual-copy instruction.
- Offline: preserve inputs, no automatic mutation retry; reconnect refreshes reads.

## Locale and accessibility

English copy, locale-aware dates/numbers, browser local-time expiry converted to RFC3339 UTC. All forms novalidate; labels, inline errors and aria-invalid, visible focus, semantic table, status live regions, skip link, password show/hide, native modal focus/inert behavior. API errors remain inline; no alert/confirm/prompt. Refresh is the shared retry action. Unsaved creation/edit data triggers a page-unload warning.
