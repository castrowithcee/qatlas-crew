# qcrew

qcrew is a program with a terminal interface for working with several AI agent command line tools, such as
Claude Code and Codex, in parallel across several projects.

**Status: early.** The terminal interface shows projects and sessions; it does not yet embed a session view,
start sessions or add projects, which the command line does. Expect breaking changes before v1.0.0.

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
qcrew                # open the terminal interface (at a terminal)
qcrew help           # overview and help topics
qcrew version        # print the version
qcrew service start  # start the background service
qcrew service status # show whether it runs (--output json for scripts)
qcrew service stop   # stop it
qcrew project add .  # register the current directory as a project
qcrew project list   # list the projects (--output json for scripts)
qcrew session start --project app --tool claude  # start Claude Code in the project app
qcrew session attach <id>                         # show the session in this terminal
qcrew inbox list                                  # what the sessions wait for
qcrew inbox answer <id> --text yes                # answer an entry; the text is typed into the session
```

## Terminal interface

`qcrew` without a command, or `qcrew tui`, opens the terminal interface when standard input and output are a
terminal; it starts the service if needed (`--no-autostart` forbids that). Without a terminal, `qcrew` shows
the overview and `qcrew tui` fails.

The projects stand on the left with the number of their open inbox entries, the sessions of the project under
the cursor on the right with tool, title, status and the time since the status last changed; both refresh
every two seconds. Below 100 columns the two stack. The areas are Projects, Inbox (a placeholder for now),
Sessions and Settings.

| Key | Action |
| --- | --- |
| `1` to `4`, or `tab` and left/right | switch the area |
| up/down, `enter` or right, `esc` or left | choose a project, open its sessions, go back |
| `f` | show the pane of the area alone and back |
| `x` | stop the session under the cursor, after asking |
| `/` | filter the list |
| `r`, `?`, `q` | read again, help, quit |

The mouse does what the keys do: click a tab, a key shown in the header or footer, or a row, and use the wheel.
With the mouse on, hold Shift to select text. Settings holds theme, language, start area, icons and the mouse;
they are kept in `tui.yaml` in `~/.qatlas/qcrew` (or in `QCREW_DIR`). If `tmux` is missing the projects still
show and the interface names the next step, `qcrew setup`. `NO_COLOR` is respected.

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
something unreadable) or `exited`. Claude Code and Codex report all five; `shell` shows `running` while it
runs and `exited` after it ended.

**Claude Code.** qcrew starts `claude` with a settings file of its own for each session, whose hooks report the
status to the service. Your user settings and the settings of the project are never changed, and the
status line that reports the context fill is added only if you have none of your own.

**Codex.** qcrew starts `codex --no-daemon` with flags for the session only and writes nothing to
`~/.codex`. Its status comes from Codex hooks, which call `qcrew hook codex`; they report the session through
the variable `QCREW_SESSION` that qcrew gives the tool. qcrew does not set up the hooks yet: until the first
hook event of a session (`signals` is then `hooks`, shown by `session list` for Codex sessions), a coarser
fallback reports through the session's flags and the screen (`signals` is `fallback`). A session is `running`
while it starts and `idle` once its screen shows that it waits for input; the notify program marks the end of a
turn as `idle`; the terminal bell of a prompt marks `waiting` with a `permission` entry whatever the prompt
asks; a new error line on the screen is `error`. The fallback cannot tell a question from a permission, and
ends `waiting` only when you answer through qcrew or the turn ends. With hooks, a failed model request is
`error` as soon as the screen shows an error line, else after ten seconds without a further event; `exited`
follows the end of the program as tmux shows it. The screen features are matched on the last lines only.
The Codex session id is recorded in the window option `@qcrew-agent-session` once Codex reports it.

## Inbox

Every Claude Code or Codex session that waits creates an inbox entry for its project: `permission` (it asks to run
something), `question` (it asks you something), `idle` (it has been idle for a minute) or `error` (its program
ended without ending its session). A session has at most one open entry. `qcrew inbox list [--project P] [--all]`
shows the entries, `qcrew inbox show <id>` one entry with the last screen lines of the session from when it
arose, and `qcrew project list` the number of open entries per project (`inbox_open`).

`qcrew inbox answer <id> --text T` types the text into the session's window and presses Enter. The answer is
always one line: line breaks become spaces, other control characters are removed, and it is limited to 1000
characters. For a `permission` entry of Claude Code the words `yes`, `always` and `no` press the key that
chooses that option instead of typing the word; for Codex every answer is typed. An entry goes from `open` to `answered` and `delivered`, and to `picked` once the
session works on; `qcrew inbox close <id>` closes it without an answer. If the session has no live window the
answer fails with `session-gone` and the entry stays open. Entries live in the memory of the service and are
gone when it stops. Text and screen lines come from the tool and are shown with control characters masked.
`qcrew help inbox-guide` has the details.

## Build from source

Go 1.26 or newer:

```sh
go build -o qcrew .
```

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## License

AGPL-3.0, see [LICENSE](LICENSE).
