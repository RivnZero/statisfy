## What changed

<!-- One or two sentences. What does this PR do? -->

## Why

<!-- Why is this needed? Link the issue if there is one. -->

## Checklist

- [ ] `gofmt -l .` is clean
- [ ] `go vet ./...` passes
- [ ] `go test ./... -count=1` passes
- [ ] `go build ./...` succeeds

For adapter changes:

- [ ] A sanitized fixture was added under `internal/adapters/fixtures/`
- [ ] Parser tests use the fixtures (no live accounts needed)
- [ ] `Source` and `Stability` are set on the Status
- [ ] Credentials are never read, printed, or serialized
- [ ] The README integration table / support matrix were updated

## Notes for reviewers

<!-- Anything unusual: internal interfaces, stability tradeoffs, behavior changes. -->
