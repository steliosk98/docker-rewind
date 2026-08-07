# Docker Rewind

**Snapshot a Docker Compose stack before you break it. Roll it back when you do.**

```console
$ rewind snapshot --tag pre-upgrade
project immich (/srv/immich/docker-compose.yml)
stopping immich
archiving volume immich_pgdata
archiving volume immich_uploads
starting immich

snapshot 2026-08-07T14-03-11Z  2 volume(s), 3.2 GB

$ docker compose pull && docker compose up -d
# ...and the new release wrecked the database schema

$ rewind restore latest
```

Volumes, image digests, and the compose file come back exactly as they were.

One static binary. Zero dependencies. No daemon, no agent, no account.

---

## Why not just back up the volumes?

Because restoring old data onto a new image is how you get a database that
won't start.

Rewind records the resolved `sha256:` digest of every service's image and
generates a throwaway `docker-compose.override.yml` at restore time that pins
them back. `:latest` moved while you weren't looking; this is the part that
makes rollback actually work.

Three more things it does that a backup script doesn't:

- **Asks Docker, not your YAML.** Volumes and services are discovered through
  `com.docker.compose.project` labels. The compose file is stored verbatim and
  replayed, never parsed — so nothing breaks when the compose spec changes.
- **Stops the stack first.** Hot-copying a live Postgres volume gives you a
  backup that restores cleanly and fails three days later. Seconds of downtime
  buys you a snapshot that's actually consistent.
- **Refuses to lie about coverage.** Bind mounts can't be captured, so Rewind
  lists them by name and marks the snapshot as partial. A backup tool that
  quietly omits data is worse than no backup tool.

## Install

```bash
go install github.com/steliosk98/docker-rewind@latest
```

Or grab a binary from [releases](https://github.com/steliosk98/docker-rewind/releases).
Requires the `docker` CLI with Compose v2. Linux, macOS, Windows.

## Use

Run from a directory containing a compose file.

```bash
rewind snapshot --tag pre-upgrade   # stop, archive volumes + digests, start
rewind list                         # what you've got
rewind restore latest               # WIPES volumes, restores the snapshot
rewind restore 2026-08-07           # by id, unique id prefix, or tag
rewind ui                           # the same three things, in a browser
```

`-C DIR` runs against a stack somewhere else. `--yes` skips the confirmation
prompt for scripts.

### GUI

`rewind ui` serves a single embedded page on `127.0.0.1:7654` and opens it.

Snapshots are stacked in depth like Time Machine: the current state sits at the
front and older ones recede into the starfield, dimming and blurring as they go.
A dated timeline runs down the right edge. Travel with the arrow keys, the scroll
wheel, `Home`/`End`, the timeline, or by clicking any receding snapshot. Each
card shows its volumes, its pinned image digests, and anything the snapshot
could not capture.

Same binary, no build step, no npm, no CDN — one `go:embed`-ed HTML file and
system fonts, because the binary makes no outbound connections. Light and dark
both verified at 4.5:1 or better, full keyboard navigation, and
`prefers-reduced-motion` drops the travel while keeping the layout.

It binds localhost only, restore makes you type the project name, and mutating
endpoints are behind a startup token — any web page you visit can POST to
localhost, and this one deletes volumes.

Don't put it behind a reverse proxy. It has no authentication and
[won't be getting any](CONTRIBUTING.md).

### Scheduling

Rewind has no scheduler. Your OS has one:

```cron
0 4 * * * cd /srv/immich && /usr/local/bin/rewind snapshot --tag nightly
```

## Where snapshots live

```
~/.docker-rewind/<project>/<timestamp>/
  manifest.json          # image digests, volume list, warnings
  compose.yml            # verbatim copy
  <volume>.tar.gz        # one per named volume
```

Plain tar and plain JSON. If this project is abandoned tomorrow, `tar xzf` still
gets your data back — that property is worth more than a clever storage format.

Directories are created `0700` on Linux and macOS. Windows ignores those bits
(NTFS uses ACLs), so snapshots there inherit the parent directory's permissions.

Override the location with `REWIND_HOME`. Archiving runs inside `alpine:3`;
override with `REWIND_HELPER_IMAGE` if you mirror your own.

## Read this before trusting it

- **Restore permanently deletes volume data.** Anything written since the
  snapshot is gone. There is no undo.
- **Snapshots are not backups.** They sit on the same host as the thing they
  protect — a dead disk takes both. Rewind protects you from bad upgrades, not
  from hardware failure. Keep a real offsite backup too.
- **Snapshots contain your secrets** in plaintext: compose files and database
  volumes, copied wholesale. Directories are `0700` and nothing is encrypted.
  Put them on an encrypted filesystem if that matters to you.
- **Bind mounts are not captured.** Rewind tells you which ones, every time.

More in [SECURITY.md](SECURITY.md).

## Scope

Rewind snapshots a Compose stack and restores it. That's the whole product, and
it's staying that way — cloud sync, encryption, scheduling, and UI auth are
deliberately out of scope because `rclone`, `restic`, `cron`, and SSH already do
them better. See the [scope charter](CONTRIBUTING.md) before opening a feature
request.

**Hard rules:** zero third-party dependencies, no outbound network connections
ever (no telemetry, no update checks), no build step, and snapshots stay
restorable with plain `tar`.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) · [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) ·
[SECURITY.md](SECURITY.md)

Apache-2.0.
