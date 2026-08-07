# Docker Rewind — MVP

Snapshot a Docker Compose stack before you break it. Roll it back when you do.

Binary: `rewind` · Repo: `docker-rewind` · License: Apache-2.0 · Zero dependencies

---

## The one thing it does

```
$ rewind snapshot --tag pre-upgrade
$ docker compose pull && docker compose up -d     # ...and it's broken
$ rewind restore latest
```

Volumes, image digests, and the compose file come back exactly as they were.

## Why it works when a backup script doesn't

**Image digests are pinned.** `:latest` moved. Restoring your volumes onto a
newer image is how you get a database that won't start. Rewind records the
resolved `sha256:` for every service and generates a `docker-compose.override.yml`
on restore that pins them back.

**Docker is the source of truth, not the YAML.** Volumes and services are
discovered via `com.docker.compose.project` labels — no compose spec parsing, so
nothing breaks when the spec changes. The compose file is stored verbatim as an
opaque blob and only replayed, never interpreted.

**Stop-first.** Hot-copying a live Postgres volume produces a backup that restores
cleanly and fails three days later. Rewind stops the stack, tars, and starts it.
Seconds of downtime, honest snapshots.

**Bind mounts are refused loudly.** Detected, listed, and explicitly reported as
NOT captured. A silent partial backup is worse than no backup.

---

## Scope

### In

| Command | Behaviour |
|---|---|
| `rewind snapshot [--tag X]` | Stop stack → tar each named volume → write manifest → start stack |
| `rewind list` | Snapshots for the current project: id, tag, age, size |
| `rewind restore <id\|latest>` | Stop → wipe + untar volumes → `compose up` with digests pinned |
| `rewind ui` | Local web GUI on `127.0.0.1:7654`, all of the above with buttons |

### Out (v0.1) — and when to add

| Deferred | Add when |
|---|---|
| `prune` / retention | Someone's disk fills. Until then: `rm -rf ~/.docker-rewind/<project>/<id>` |
| `diff` between snapshots | v0.2. Cheap (manifest vs manifest), good demo value, not load-bearing |
| restic / dedup backend | Full-tar disk usage becomes a real complaint |
| Scheduling | Never. README ships a cron line |
| Encryption, remote/cloud sync | Never. Filesystem, restic, and rclone already exist |
| Multi-host, auth, TLS on the UI | Never in this project's scope. UI binds localhost only |
| Live/hot snapshots (no downtime) | A real use case shows up with a plan for consistency |
| CRIU process checkpointing | Never |

---

## Design

### Storage layout

```
~/.docker-rewind/<project>/<id>/
  manifest.json
  compose.yml            # verbatim copy
  <volume>.tar.gz        # one per named volume
```

`id` is the UTC timestamp `2026-08-07T14-03-11Z`. Plain tar — if Rewind ever
disappears, `tar xzf` still restores your data. That property is worth more than
compression ratio.

### Manifest

```json
{
  "version": 1,
  "id": "2026-08-07T14-03-11Z",
  "tag": "pre-upgrade",
  "project": "immich",
  "compose_path": "/srv/immich/docker-compose.yml",
  "services": [
    { "name": "server", "image": "ghcr.io/immich-app/immich-server:v1.9",
      "digest": "sha256:ab12..." }
  ],
  "volumes": [
    { "name": "immich_pgdata", "file": "immich_pgdata.tar.gz", "bytes": 918273645 }
  ],
  "bind_mounts": ["/srv/immich/photos"],
  "warnings": ["3 bind mounts were NOT captured"]
}
```

### Volume capture

Stream to host stdout rather than mounting an output directory — avoids
bind-mount permission surprises on Windows and macOS entirely:

```
docker run --rm -v <vol>:/v alpine tar cz -C /v . > <vol>.tar.gz
```

Restore is the same trick in reverse, with the volume emptied first.

### Digest pinning on restore

Generate `docker-compose.override.yml` with `image: repo@sha256:...` per service,
then `docker compose -f <original> -f <override> up -d`. Compose merges it. No
rewriting of the user's file, ever.

### GUI

Same binary. `rewind ui` serves an embedded page (`go:embed`) on localhost and
opens the browser.

- Vanilla HTML + a little JS. No React, no npm, no build step, no CDN.
- JSON API: `GET /api/snapshots`, `POST /api/snapshot`, `POST /api/restore`.
- Polling, not websockets.
- Restore requires typing the project name to confirm — it's destructive.
- Binds `127.0.0.1` only. No login: the trust boundary is local access.
- **CSRF guard, not optional.** Any page you visit can `POST` to `localhost`, and
  `/api/restore` deletes volumes. Mutating endpoints require a random token
  generated at startup (put in the URL `rewind ui` opens, echoed back in a
  header) and reject any request carrying a cross-origin `Origin`. `GET` never
  mutates. ~15 lines; skipping it makes browsing the web while Rewind is open a
  data-loss risk.

If a build step ever appears in this project, something has gone wrong.

---

## Files

```
docker-rewind/
  go.mod            # module github.com/<you>/docker-rewind — no external deps
  main.go           # stdlib flag, switch on argv[1]              ~80 loc
  docker.go         # shell out to docker: project, volumes, digests ~120
  snapshot.go       # snapshot + manifest                          ~120
  restore.go        # restore + override generation                ~100
  ui.go             # http server + JSON API                       ~120
  ui/index.html     # embedded GUI                                 ~200
  README.md  LICENSE  .gitignore
```

~550 lines of Go, ~200 of HTML, zero third-party dependencies. No cobra — stdlib
`flag` covers four subcommands. Everything shells out to the `docker` CLI, which
means compose v2 support is free and there's no SDK version treadmill.

---

## Ship checklist

1. `snapshot` → `list` → `restore` round-trips a real stack (Postgres + app)
2. Bind-mount warning fires and is impossible to miss
3. `rewind ui` does the same three things
4. README opens with a ~6-line asciinema: snapshot → break it on purpose →
   `rewind restore latest` → working
5. Apache-2.0, GitHub Actions cross-compile release for linux/darwin/windows

Item 4 is not decoration. If that demo isn't compelling the project gets no
traction regardless of the code.
