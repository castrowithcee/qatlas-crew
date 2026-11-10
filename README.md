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
qcrew project add .  # register the current directory as a project
qcrew project list   # list the projects (--output json for scripts)
qcrew session start --project app --tool claude  # start Claude Code in the project app
qcrew session attach <id>                         # show the session in this terminal
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

## Projects

A project is a directory registered by its path: `qcrew project add <path> [--name N]` stores it with symbolic
links resolved, under a random 12-character id and a unique name (by default the directory's base name).
`--create` makes a missing directory. `qcrew project remove <id|name> --confirm` takes it off the list and
deletes nothing. qcrew requires nothing inside the directory, neither Git nor any other file.

The list lives in `~/.qatlas/qcrew/config.yaml`; `--config`, `QCREW_CONFIG` or `QCREW_DIR` choose another file.
It is plain YAML for editing by hand and read strictly: unknown keys and other schema versions are errors.
The qcrew service is its only writer; it locks the file, keeps it private to your user and saves the replaced
version in `backups/` beside it first. `qcrew help project-guide` describes the format.

## Sessions

An agent session runs `claude`, `codex` or your login shell (`shell`) in a project's directory:
`qcrew session start --project P --tool T [--title X]`. Sessions run in tmux on a server of their own,
`tmux -L qcrew`, one tmux session per project and one window per agent session; your own tmux server is
never touched. They keep running when the service stops or the terminal closes.
`qcrew session list [--project P]` shows them with a status, since when they have it (`since`) and how full
the tool's context window is (`context_percent`, empty while unknown);
`qcrew session stop <id> --confirm` closes the window; `qcrew session attach <id>` prints the tmux command
that shows a session and runs it at a terminal. A project with running sessions cannot be removed.
qcrew needs `tmux` and the tool in `PATH`. `QCREW_TMUX_SOCKET` (an absolute socket path) moves qcrew to
another tmux server, for example in tests. `qcrew help session-guide` has the details.

The status is one of `running` (the tool works), `waiting` (it asks for a permission or an answer), `idle`
(it finished its turn and waits for input), `error` (its program ended without saying so, or a hook sent
something unreadable) or `exited`. Only Claude Code reports all five; `codex` and `shell` show `running`
while the tool runs and `exited` after it ended.

**Claude Code.** qcrew starts `claude` with a settings file of its own for each session, whose hooks report the
status to the service. Your user settings and the settings of the project are never changed, and the
status line that reports the context fill is added only if you have none of your own.

## Build from source

Go 1.26 or newer:

```sh
go build -o qcrew .
```

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## License

AGPL-3.0, see [LICENSE](LICENSE).
