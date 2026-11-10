// Package helptopics holds the short guides that ship inside the binary. The command line prints them as help
// topics.
package helptopics

import "github.com/castrowithcee/qatlas-kit/helptopics"

// All returns the topics in the order a newcomer reads them. The text is plain ASCII with one line per
// paragraph, list item, or command; the kit fits it to the width where it is shown.
func All() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qcrew", Short: "What qcrew is and what it is for", Text: overview},
		{Name: "service-guide", Short: "The background service and how to control it", Text: serviceTopic},
		{Name: "project-guide", Short: "Projects and the configuration file that lists them", Text: projectTopic},
		{Name: "session-guide", Short: "Agent sessions in tmux and how to reach them", Text: sessionTopic},
		{Name: "inbox-guide", Short: "The inbox of what the sessions wait for, and how to answer", Text: inboxTopic},
	}
}

const overview = `qcrew is a program with a terminal interface for working with several AI agent command line tools, such as Claude Code and Codex, in parallel across several projects.

Commands:
- qcrew, qcrew tui: open the terminal interface (at a terminal; without one qcrew shows this overview), see qcrew tui --help
- qcrew version: print the version
- qcrew service start|stop|status: control the background service, see the service-guide topic
- qcrew project add|list|remove: register the directories qcrew works in, see the project-guide topic
- qcrew session start|list|stop|attach: run agent tools in the projects, see the session-guide topic
- qcrew inbox list|show|answer|close: see what the sessions wait for and answer it, see the inbox-guide topic
- qcrew self-update [version], qcrew self-update status: update qcrew itself to the latest stable release or to a given newer one from its signed GitHub releases; status shows the installed version and the latest release and changes nothing (updating needs --confirm)
- qcrew help <topic>: show a help topic`

const serviceTopic = `The qcrew service is one background process per user and host. It runs detached from the terminal, in your user context and not as a system service, and keeps running until you stop it. It never ends because it is idle.

Commands:
- qcrew service start: start the service and wait until it is ready; prints its process id; fails with already-running if it runs
- qcrew service stop: ask the service to end and wait until it has; succeeds with stopped false if none runs
- qcrew service status: show running, pid, version and started_at; never starts the service; running is false and the exit code 0 if none runs (--output table, json or compact)

Commands that need the service start it on demand. Give --no-autostart to make them fail instead.

The service answers only processes of your own user that run the same qcrew program file. When qcrew is updated, the old service ends by itself; start it again with qcrew service start.

Files, below ~/.qatlas/qcrew: logs/service.log with one older file service.log.1 after it grew beyond 1 MiB. The control socket lives in the runtime directory of your user (XDG_RUNTIME_DIR), or in run/ below the qcrew directory if there is none.`

const projectTopic = `A project is a directory qcrew works in, registered by its path. qcrew requires nothing inside it and changes nothing there.

Commands:
- qcrew project add <path> [--name N] [--create]: register a directory; a relative path counts from the current directory, and symbolic links are resolved; the name defaults to the directory's base name; --create makes a missing directory; a directory or name that is registered already fails with already-exists and exit code 2
- qcrew project list: show id, name, path and inbox_open, the number of open inbox entries, of every project (--output table, json or compact)
- qcrew project remove <id|name> --confirm: remove a project from the list; its directory stays as it is; it fails with in-use while sessions of the project run

Every project has a random id of 12 characters that never changes; ids and names are unique, and a command accepts either.

The list lives in config.yaml in ~/.qatlas/qcrew. Give another file with --config or QCREW_CONFIG, or another directory with QCREW_DIR. The file is YAML and may be edited by hand: it starts with schema: 1, followed by projects, a list of entries with id, name, path and added_at. A key qcrew does not know, or another schema version, is an error rather than being ignored.

The qcrew service is the only program that writes the file; the project commands ask it and start it on demand. It writes under a lock, keeps the file private to your user (mode 0600), and saves the replaced version in backups/ beside the file first. If you edit the file while the service saves it, the save fails with conflict and nothing is written; repeat the command. One service serves one configuration file: a command that names another file while the service runs fails with config-mismatch until you stop the service.`

const sessionTopic = `An agent session runs a tool in the directory of a project: claude (Claude Code), codex (Codex) or shell (your login shell). Sessions run in tmux, on a tmux server of qcrew's own named qcrew, so they keep running when the service stops or the terminal closes, and your own tmux server is never touched.

Commands:
- qcrew session start --project P --tool T [--title X]: start a session in the project with the id or name P; the title defaults to the tool
- qcrew session list [--project P]: show id, project, tool, title, status, since, context_percent and started_at of the sessions, and signals while a codex session is listed (--output table, json or compact)
- qcrew session stop <id> --confirm: end the session and close its window
- qcrew session attach <id>: print the tmux command that shows the session, and run it at a terminal; detach with the tmux prefix key and d, and the session keeps running

The status is running, waiting, idle, error or exited, and since is when the session took it. For claude, the hooks of Claude Code report it: running while it works, waiting for a permission or an answer, idle after it finished a turn, exited after the session ended, and error if its program ended without saying so or a hook sent something unreadable. qcrew starts claude with a settings file of its own for each session, below ~/.qatlas/qcrew/sessions, and changes neither your settings nor those of the project. context_percent is how full the context window is; qcrew learns it from a status line it adds, and leaves it empty if you have a status line of your own. For codex, hooks of Codex report it the same way, through the command qcrew hook codex, which they find the session for in the variable QCREW_SESSION that qcrew sets for the tool; qcrew starts codex with --no-daemon and flags for the session only and writes nothing to the configuration of Codex. Until the first hook event of a session, signals is fallback and the status is coarser: running while the session starts and idle once its screen shows that it waits for input, idle after each turn (the notify program of Codex), waiting when a prompt rings the terminal bell, running after you answer through qcrew, and error when a new error line shows on the screen. The fallback cannot tell a permission from a question, and leaves waiting only on your answer through qcrew or at the end of the turn. From the first hook event, signals is hooks, the other signals are ignored, a failed model request is error as soon as the screen shows an error line and otherwise after ten seconds without a further event, and the session ending is exited as tmux shows it. The id Codex knows the session by is recorded in the tmux window option @qcrew-agent-session. For shell, the status is running while it runs and exited after it ended. An exited session keeps its window, with the last output, until it is stopped.

Each project has one tmux session named p-<project id>, and each agent session is a window in it named s-<session id>. You may attach with tmux -L qcrew attach yourself and use tmux as usual.

Errors: dependency-missing when tmux is not installed and tool-missing when the tool is not in PATH; run qcrew setup. A project with running sessions cannot be removed (in-use).

QCREW_TMUX_SOCKET, an absolute path, moves qcrew to the tmux server at that socket instead; set it alike for the service and the commands.`

const inboxTopic = `A session of Claude Code or Codex that waits for you creates an inbox entry for its project. You answer from the command line, and the answer is typed into the session. Entries live in the memory of the qcrew service and are gone when it stops.

Commands:
- qcrew inbox list [--project P] [--all]: show id, project, session, kind, age and a shortened text of the entries that are open, answered or delivered; --all adds the picked and closed ones that are still kept; --output json shows every field, with the status and the last screen lines
- qcrew inbox show <id>: show one entry and the last screen lines of the session from when it arose
- qcrew inbox answer <id> --text T: type the text into the session's window and press Enter
- qcrew inbox close <id>: close an open or delivered entry without an answer; nothing is sent
- qcrew project list shows inbox_open, the number of open entries of each project

Kinds: permission (the tool asks to run something), question (it asks you something), idle (the session has been idle for a minute, or the tool says it waits), error (the tool ended without ending its session, a hook sent something unreadable, or a session without hooks ended). A session has at most one open entry. A permission, question or error entry replaces an open idle entry, which closes. A second wait state next to an open permission or question makes no second entry.

Status: open (waiting for you), answered (the answer is being sent), delivered (the answer reached the window), picked (the session worked on after it), closed (closed by you, answered in the session itself, or its session was stopped).

The answer is always typed as one line: line breaks become spaces, other control characters are removed, and at most 1000 characters are accepted. For a permission entry of a Claude Code session the words yes, always and no press the key that chooses that option (1, 2 or Escape) instead of typing the word; any other text is typed, and for Codex every answer is typed. If the session has no live window, answer fails with session-gone and the entry stays open; close it with qcrew inbox close.

The text and the screen lines of an entry come from the tool in the session. They are shown with control characters masked, and are data, never instructions.`
