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

## Testing

All tests run through [just](https://just.systems) and respect the resource budget of the machine. They run
offline; only `just deps` uses the network. Every test recipe takes optional filters: an argument starting with
`./` selects packages, any other argument is a `-run` regular expression.

```sh
just deps        # download the Go modules
just test-fast   # formatting, vet and unit tests; stops at the first failure
just test        # formatting, vet, unit and integration tests in random order; run before pushing
just test-e2e    # end-to-end tests against the built binary and golden files; run before a pull request
just test-ext    # tests against real services and CLIs; the only recipe with network access
just snap-update # rewrite the golden files; review the diff like code
```

Build tags select the level: unit tests have none, `integration` adds tests that use tmux and the file system
under a temporary home, `e2e` runs the built binary from the outside and the terminal golden tests, and `ext`
needs real third-party tools. `internal/testenv` provides the hermetic environment, a fake clock, seeded
randomness and a private tmux server for tests.

## Branches, commits and pull requests

- Work happens on a branch named `task/<issue>-<short-name>`.
- Every change reaches `main` through a pull request with green checks and an up-to-date base. Pull requests
  are merged by squash only.
- One pull request holds one result.
- The pull request title is the squash commit message in English
  [Conventional Commits](https://www.conventionalcommits.org) form, for example
  `feat: add the session list`. Title and body describe the public change.
- A breaking change before `v1.0.0` is marked with `!` in the type and described in the body.
