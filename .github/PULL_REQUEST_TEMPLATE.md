## What this does and why

<!-- Summarize the change and the motivation. Link the issue this addresses, if any (e.g. "Closes #12"). -->

## How to verify

<!-- What did you run locally? What should a reviewer check? -->

## Checklist

- [ ] `go build ./... && go vet ./... && go test ./... -race` pass (backend changes)
- [ ] `npm run typecheck && npm run lint && npm run test && npm run build` pass, from `web/` (frontend changes)
- [ ] New behavior has test coverage
- [ ] No unrelated formatting/refactor changes mixed into this diff
