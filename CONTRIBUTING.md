# Contributing to groundtruth

Thanks for considering a contribution. groundtruth is young, so the process below is intentionally lightweight — it'll grow if and when it needs to.

## Before you start

For anything beyond a small fix (a new feature, a behavior change, a new dependency), please open an issue first and wait for a maintainer to weigh in before writing code. This avoids spending time on something that doesn't fit the project's scope or that's already being worked on. For typos, docs fixes, and small obvious bugs, a PR without a prior issue is fine.

## Branching and pull requests

- `main` is always releasable. All work happens on a feature branch off `main` and lands via pull request — there's no separate development branch.
- Branch names aren't enforced, but `type/short-description` (e.g. `feat/workspace-crud`, `fix/session-expiry-off-by-one`) is the convention used in this repo.
- Keep PRs focused: one logical change per PR. A bug fix shouldn't carry along an unrelated refactor.
- CI (lint, tests, security scanning) must pass before a PR can merge. `main` is protected — even maintainers go through a PR.
- Write commit messages that explain *why*, not just *what*; the diff already shows what changed.

## Running things locally

```sh
go build ./...
go vet ./...
go test ./... -race
golangci-lint run

cd web
npm ci
npm run typecheck
npm run lint
npm run format
npm run test
npm run build
```

`make build-frontend && make build` builds the frontend and embeds it into the Go binary, matching what CI and releases do. See the [README](README.md) for running the result.

## Code style

- Go: standard `gofmt`/`gofumpt` formatting (enforced by `golangci-lint run`, not a matter of personal preference). No comments that just restate what the code does — only comments that explain a non-obvious *why*.
- TypeScript/React: Prettier-formatted, functional components, TanStack Query for all server data (no ad-hoc `fetch` calls in components).
- Tests matter more than coverage percentage. A real temporary SQLite database is used in Go tests instead of mocking the database layer — see `internal/store/storetest`.

## Security issues

Please don't open a public issue for a security vulnerability — see [SECURITY.md](SECURITY.md) instead.
