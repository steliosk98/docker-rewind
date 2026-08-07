# Security Policy

## Reporting a vulnerability

Do **not** open a public issue.

Use GitHub's [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on this repo, or email the maintainer.

Expect an acknowledgement within 72 hours and a fix or a plan within 14 days.
Please give us 90 days before public disclosure. Credit in the advisory unless
you'd rather not have it.

**Supported:** the latest release only. There are no backports.

---

## What this tool can do to you

Be honest about the blast radius before trusting it.

**It needs the Docker socket.** Access to `/var/run/docker.sock` is equivalent to
root on the host. That is inherent to doing anything with Docker, not a Rewind
design choice, but it means a compromise of this binary is a compromise of the
host.

**Restore permanently deletes volume data.** `rewind restore` empties every named
volume in the project before unpacking the snapshot. Anything written since the
snapshot is gone. There is no undo.

**Snapshots contain your secrets.** The compose file is copied verbatim and
database volumes are copied wholesale, so snapshot directories routinely hold
passwords, tokens, and user data in plaintext. Rewind requests `0700` on
`~/.docker-rewind/` and on every snapshot directory, and does not encrypt
anything.

**On Windows that `0700` does not apply.** Go's permission bits do not map onto
NTFS ACLs, so snapshot directories inherit the ACL of their parent — in practice
`755`-equivalent. If other accounts on the machine matter to you, put
`REWIND_HOME` somewhere you have restricted yourself. On Linux and macOS the
`0700` is real.

If you need encryption at rest, put the snapshot directory on an encrypted
filesystem. That is a deliberate scope decision, not an oversight.

**Snapshots are not backups.** They live on the same host as the thing they
protect. A dead disk takes both. Rewind protects you from bad upgrades, not from
hardware failure. Use a real offsite backup as well.

---

## The local web UI

`rewind ui` starts an HTTP server. Its restore endpoint destroys data, so it gets
treated as a privileged interface:

- Binds `127.0.0.1` only. Never `0.0.0.0`. A PR changing this will be rejected.
- No TLS and no login, by design — the trust boundary is "local access".
- **CSRF is a real threat here.** Any web page you visit can `POST` to
  `localhost`. Mutating endpoints therefore require a random session token
  generated at startup and passed in a header, and reject requests carrying a
  cross-origin `Origin`. `GET` never mutates anything.
- Destructive actions require typing the project name to confirm.

Do not put this behind a reverse proxy to "share it with the team". It has no
authentication, and adding some is explicitly out of scope. If you need remote
access, use SSH.

---

## Supply chain

The project's defence here is having nothing to attack:

- **Zero third-party dependencies.** `go.mod` has no `require` block.
- **No outbound network connections.** No telemetry, no update checks, no crash
  reporting. The binary talks to `127.0.0.1` for the UI and to the local `docker`
  CLI. Nothing else.
- **No build step.** `go build ./...` is the whole pipeline. Nothing is fetched,
  generated, or minified.

These are enforced in [CONTRIBUTING.md](CONTRIBUTING.md) as hard rules. They exist
so that verifying what this tool does with your data stays a job you can finish in
an afternoon.

Release binaries are built by GitHub Actions from tagged commits. Verify checksums
against the release page, or `go build` it yourself — it takes seconds and there's
nothing to download.
