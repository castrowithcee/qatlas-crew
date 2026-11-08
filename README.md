# qcrew

qcrew is a program with a terminal interface for working with several AI agent command line tools, such as
Claude Code and Codex, in parallel across several projects.

**Status: early.** The command line skeleton exists; the terminal interface and the session handling follow.
Expect breaking changes before v1.0.0.

## Installation

Install the latest release into `~/.local` with the signed installer (needs `ssh-keygen`):

```sh
curl -fsSL https://github.com/castrowithcee/qatlas-crew/releases/latest/download/install.sh | sh
```

The script verifies the release signature and checksum before it installs anything. Set `QCREW_VERSION` to
install a specific release, for example `QCREW_VERSION=v0.1.0`. Make sure `~/.local/bin` is on your `PATH`,
then run `qcrew --version`.

Update an installation with `qcrew self-update --confirm`; `qcrew self-update status` shows whether a newer
release exists.

## Usage

```sh
qcrew help           # overview and help topics
qcrew version        # print the version
qcrew service start  # start the background service
qcrew service status # show whether it runs (--output json for scripts)
qcrew service stop   # stop it
```

## Service

qcrew keeps one background service per user and host. It runs detached from the terminal in your user
context (it is not a system service) and keeps running until `qcrew service stop`. `qcrew service start`
waits until it is ready and prints its process id; a second start fails with `already-running`.
`qcrew service status --output json` reports `running`, `pid` and `version`, and never starts the service.
Commands that need the service start it on demand; `--no-autostart` makes them fail instead.

The service listens on a local socket in your runtime directory that only processes of your own user running
the same qcrew program file can use. After `qcrew self-update` the old service ends by itself; start it
again. Its log is `~/.qatlas/qcrew/logs/service.log`, rotated to `service.log.1` above 1 MiB.

## Build from source

Go 1.26 or newer:

```sh
go build -o qcrew .
```

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## License

AGPL-3.0, see [LICENSE](LICENSE).
