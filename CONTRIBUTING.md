# Contributing

## Design

- **Layers:** `internal/cli` is the command tree, `internal/tui` the terminal interface, `internal/control`
  the wire protocol (standard library only), `internal/client` the calls to the service and
  `internal/service` the service. The domain packages (`project`, `session`, `adapter`, `inbox`, `team`,
  `store`, `notify`, `setup`) import none of these layers, and the terminal interface reaches the domain only
  through the client. `architecture_test.go` enforces this; the test is the reference.
- **Documentation:** Each package states its purpose in a package comment, an empty placeholder package in
  `doc.go`. Comments are English and explain only a non-obvious why. User documentation states the current
  contract only; history belongs in Git and release notes.
- **Tests:** Tests need no network, no real home directory and no fixed ports. Test data is invented and
  contains no secrets, real hosts or personal data.
- **Dependencies:** A new dependency needs a reason in the pull request and an exact version in `go.mod`.

## Local checks

Run all checks for the whole module before pushing:

```sh
gofmt -l .                # must print nothing
go vet ./...
go test -count=1 ./...
```

## Branches, commits and pull requests

- Work happens on a branch named `task/<issue>-<short-name>`.
- Every change reaches `main` through a pull request with green checks and an up-to-date base. Pull requests
  are merged by squash only.
- One pull request holds one result.
- The pull request title is the squash commit message in English
  [Conventional Commits](https://www.conventionalcommits.org) form, for example
  `feat: add the session list`. Title and body describe the public change.
- A breaking change before `v1.0.0` is marked with `!` in the type and described in the body.
