<img src="icon.png" width="96" align="left" alt="BookBridge logo">

# shelfarr-bookbridge

Watches your Goodreads shelves and creates **ebook** download requests in
[Shelfarr](https://shelfarr.org). Self-hosted, single Docker container with a web GUI.

<br clear="left">


## How it works

`Goodreads shelf -> dedup (SQLite) -> resolve in Shelfarr -> POST /api/v1/requests (ebook)`,
then reconciles each request's status back. Reads via RSS (<=100 items, public or with a
feed key) or an authenticated HTML reader (private and/or >100 items, via a session cookie).

## Setup

1. **Shelfarr token:** Shelfarr -> Profile -> API tokens -> create one with scopes
   `search:read`, `requests:write`, `requests:read`. It looks like `shf_...`.
2. **Goodreads:** find your numeric user id (in your profile URL). For a private shelf or one
   with >100 items, copy your browser session **cookie** (DevTools -> Network -> any
   goodreads.com request -> Request Headers -> `Cookie`) into `GOODREADS_COOKIE`. For a
   public/small shelf you can instead use the RSS `GOODREADS_FEED_KEY`.
3. Configure env (see `docker-compose.yml`) and start the container.

## Commands

- `bookbridge sync --dry-run` — preview what would be requested (default).
- `bookbridge sync --apply` — create requests.
- `bookbridge sync --baseline` — mark current shelf contents as seen (no requests); run this
  first so an existing backlog isn't requested all at once.
- `bookbridge daemon` — run on a schedule (default hourly); `--once` runs a single cycle.

## Web GUI

Browse to `http://<host>:7373`. Auth mirrors *arr: set `AUTH_REQUIRED=local` to skip
login from your LAN (private/loopback addresses) and require it from outside, or
`enabled` to always require it; `AUTH_METHOD=none` disables it entirely. The admin
user/password are seeded from `AUTH_USERNAME`/`AUTH_PASSWORD` on first start, then
managed in Settings. All parameters are editable in the GUI and persist in `/config`
(overriding env). The container runs the daemon and the GUI together.

## Notes

- The Goodreads session cookie expires periodically — re-grab it when you see a
  "cookie expired" error.
- Language is inferred from the title (English/Spanish) and sent as a soft preference;
  disable with `LANG_INFERENCE=off`.
- Automating a private Goodreads shelf may conflict with Goodreads' Terms; this is for your
  own account and data.
