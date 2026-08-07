## What problem does this solve?

<!-- The problem, not the diff. If there's an issue, link it. -->

## How

<!-- One or two sentences. -->

---

## Checklist

- [ ] Read the scope charter in CONTRIBUTING.md; this isn't in the rejected table
- [ ] `go.mod` still has no `require` block — **zero dependencies**
- [ ] No outbound network calls added
- [ ] No build step added
- [ ] `gofmt -l .` prints nothing, `go vet ./...` is clean
- [ ] `go test ./...` passes
- [ ] Tested against a real stack with real data, not just compiled
- [ ] README updated if behaviour changed

### If this touches restore or volume deletion

- [ ] There is a test that fails if the logic breaks
- [ ] Data that can't be captured or restored is still reported loudly, by name

<!--
Restore wipes volumes. There's no undo for the undo. If you're unsure whether
your change is safe, say so here — that's a normal thing to write, not a
weakness in the PR.
-->
