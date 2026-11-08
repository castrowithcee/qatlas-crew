# Test entry points; every test recipe runs through scripts/test-run, which applies the host test budget.
# Optional filters: an argument starting with "./" selects packages, any other argument is a -run regexp,
# for example `just test ./internal/cli/...` or `just test-fast TestVersion`.
set positional-arguments

run := "scripts/test-run"

# Downloads the Go modules; the only recipe that needs the network.
deps:
    @go mod download

# Formatting, vet and unit tests; stops at the first failure.
test-fast *args:
    @TEST_SLOW=5 {{run}} scripts/test-go fast "$@"

# Formatting, vet, unit and integration tests in random order; run before pushing.
test *args:
    @TEST_SLOW=5 {{run}} scripts/test-go full "$@"

# End-to-end tests against the built binary and the golden files of the terminal interface.
test-e2e *args:
    @TEST_KIND=e2e TEST_SLOW=5 {{run}} scripts/test-go e2e "$@"

# Tests against real services and CLIs; the only test recipe with network access.
test-ext *args:
    @TEST_NET=1 TEST_SLOW=5 {{run}} scripts/test-go ext "$@"

# Rewrites the golden files of the terminal interface and the command line output; review the diff like code.
snap-update:
    @{{run}} scripts/test-go snap
