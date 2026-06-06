# Contributing to axi-go

Thanks for considering a contribution. axi-go aims to stay small, principled, and dependency-free — please keep that in mind when proposing changes.

## Core principles

1. **Zero external dependencies.** The `domain/` package must not import anything outside the standard library. Adapters in `inmemory/`, `jsonstore/` etc. also use stdlib only.
2. **DDD boundaries.** Respect the dependency direction: `domain` ← `application` ← `inmemory`/`jsonstore` ← `axi` (root facade) ← consumer code. The domain owns its port interfaces.
3. **No delivery mechanisms in this repo.** axi-go is a library, not a service. No HTTP, gRPC, CLI, or MCP code belongs here. Build adapters in your own repo.
3. **Aggregates enforce invariants.** Unexported fields, constructor validation, defensive copies, state-machine transitions.
4. **Action failure is a valid outcome**, not a Go error. Infrastructure errors are `error`; domain failures transition the session to `Failed`.

## Development setup

```bash
git clone https://github.com/klarlabs-studio/axi-go.git
cd axi-go
make install-hooks   # installs git pre-commit hook
make check           # fmt + lint + test + security
```

You need Go 1.26+, `golangci-lint` v2, and optionally `nox` + `coverctl` for the full `make check`.

## Commit style

Use [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation only
- `test:` tests only
- `refactor:` no behavior change
- `chore:` tooling / build

Atomic commits — one logical change per commit. The pre-commit hook runs fmt, lint, vet, and all tests.

### Sign-offs (DCO)

All commits must be signed off with `git commit -s`, which appends a
`Signed-off-by: Your Name <your@email>` line and certifies the
[Developer Certificate of Origin](https://developercertificate.org/) —
a lightweight attestation that you have the right to submit the patch
under the project's license. No CLA to sign; the sign-off is the
paper trail.

## Pull request checklist

- [ ] `make check` passes locally
- [ ] Tests added for new behavior (TDD preferred)
- [ ] Race detector clean: `go test ./... -race`
- [ ] README/CLAUDE.md updated if public API changed
- [ ] Commit messages follow conventional format

## Testing philosophy

- Domain tests use in-package fakes (no `inmemory/` dependency)
- `axi` package tests drive the full kernel programmatically
- Integration tests live in the package they exercise, not a separate `test/` dir
- Table-driven tests where there are many small cases
- Concurrent/async code gets a dedicated race-detector test

## Adding new features

Before opening a PR for a substantive change:

1. Open an issue describing the problem and proposed solution
2. Wait for discussion / approval
3. Start with failing tests
4. Keep the PR focused — one feature per PR

## Reporting bugs

Include:
- Go version (`go version`)
- Minimal reproduction
- Expected vs actual behavior
- Full error message and stack trace

## Code review

All changes go through review. Reviewers will check:
- Test coverage
- Invariant preservation (no broken aggregate constraints)
- Dependency hygiene (no new external deps without discussion)
- Documentation accuracy
