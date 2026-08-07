# Contributing to Docker Rewind

Thanks for being here. Read the scope charter before writing code — it is the
part of this document that will actually get your PR merged or closed.

---

## Scope charter

Docker Rewind snapshots a Docker Compose stack and restores it. That is the
entire product. It is deliberately small, and keeping it small is a feature, not
a phase we are going to grow out of.

### In scope

- Correctness and safety of snapshot / restore
- Bugs on Linux, macOS, Windows + Docker Desktop
- Clearer errors, especially around data that is *not* captured
- Docs, tests, packaging
- The four commands: `snapshot`, `list`, `restore`, `ui`

### Rejected by default

These are not "not yet". They are decisions. A PR implementing one will be closed
with a link to this section unless it was agreed in an issue first.

| Proposal | Why not |
|---|---|
| Cloud / S3 / remote sync | `rclone` exists and is better at it |
| Encryption | Your filesystem or `restic` already does this |
| Scheduling, daemon, cron replacement | Your OS has a scheduler. The README shows the line |
| Web UI auth, TLS, multi-user | The UI binds `127.0.0.1`. If you need auth, you are exposing it, which is out of scope |
| React/Vue/Svelte, npm, any build step | The GUI is one embedded HTML file. This is not negotiable |
| Kubernetes, Swarm, Podman, nerdctl | Different products. Fork freely |
| CRIU / live process checkpointing | Fragile, upstream is barely maintained |
| Telemetry, analytics, update checks | See "No network" below |
| Plugin systems, hooks, extension APIs | One implementation does not need an interface |

If you think one of these is genuinely essential, open an issue and make the
case. Changing this table is allowed. Ignoring it is not.

---

## Hard rules

These are enforced in review and in CI. A PR that breaks one does not get merged
no matter how good the feature is.

**1. Zero third-party dependencies.**
`go.mod` has no `require` block and it stays that way. The standard library plus
the `docker` CLI covers everything this tool does. A dependency in a backup tool
is a supply-chain hole in something people trust with their data.

**2. No outbound network connections. Ever.**
The binary must never open a socket to anything but `127.0.0.1` for the UI. No
update checks, no telemetry, no crash reporting, no fetching anything. People run
this against production data on private hosts. Auditing this claim must stay a
five-minute job.

**3. No build step.**
`go build ./...` produces the entire product, GUI included. No node, no bundler,
no generated assets checked in beyond `go:embed`.

**4. Destructive paths need a test.**
Anything touching `restore.go` or volume deletion ships with a test that fails if
the logic breaks. Restore wipes volumes. There is no undo for the undo.

**5. Never silently skip data.**
If Rewind cannot capture something — bind mounts, external volumes, anything —
it says so loudly, by name, and marks the snapshot as partial. A backup tool that
quietly omits data is worse than no backup tool. Code that makes an omission
quieter will be rejected.

**6. Stay restorable without this tool.**
Snapshots are plain `tar.gz` plus a JSON manifest. If Docker Rewind is abandoned
tomorrow, `tar xzf` must still get people their data back. No custom container
formats, no binary manifests, no compression that needs our code to read.

---

## Working on it

```bash
go build ./...
go test ./...
go vet ./...
gofmt -l .          # must print nothing
```

You need Docker running to test for real. Integration tests use a throwaway
Postgres + app stack under `testdata/`.

Test your change against an actual stack with actual data before opening the PR.
"It compiles" is not evidence for software whose failure mode is losing someone's
photo library.

---

## Pull requests

- One change per PR. A refactor bundled with a feature gets asked to split.
- Branch off `main`. Rebase, don't merge.
- Explain the *problem* in the description, not just the diff.
- New behaviour needs a README line. Undocumented features do not exist.
- Small PRs get reviewed in days. 900-line PRs get reviewed eventually.

Commits should be readable. `fix: don't crash when a volume has no data` beats
`fixes`. Conventional Commits are welcome but not enforced.

By opening a PR you agree your contribution is licensed under Apache-2.0. No CLA.

---

## Issues

Bugs: include your OS, `docker version`, the exact command, and the full output.
A redacted `manifest.json` from the failing snapshot is usually what solves it.

Features: check the rejected table above first, then open an issue *before*
writing code. Getting a "we won't take this" after you spent a weekend on it is a
bad experience and it is avoidable.

Security issues do **not** go in the issue tracker — see [SECURITY.md](SECURITY.md).

---

## Reviews

One maintainer. Reviews come in days, not hours, and the default answer to new
surface area is no. That is not personal — it is how a tool people trust with
their data stays trustworthy. Bug fixes and correctness work get merged fast.
