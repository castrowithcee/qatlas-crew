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
	}
}

const overview = `qcrew is a program with a terminal interface for working with several AI agent command line tools, such as Claude Code and Codex, in parallel across several projects. It is in an early state: the terminal interface is not available yet.

Commands:
- qcrew version: print the version
- qcrew service start|stop|status: control the background service, see the service-guide topic
- qcrew project add|list|remove: register the directories qcrew works in, see the project-guide topic
- qcrew session start|list|stop|attach: run agent tools in the projects, see the session-guide topic
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
- qcrew project list: show id, name and path of every project (--output table, json or compact)
- qcrew project remove <id|name> --confirm: remove a project from the list; its directory stays as it is; it fails with in-use while sessions of the project run

Every project has a random id of 12 characters that never changes; ids and names are unique, and a command accepts either.

The list lives in config.yaml in ~/.qatlas/qcrew. Give another file with --config or QCREW_CONFIG, or another directory with QCREW_DIR. The file is YAML and may be edited by hand: it starts with schema: 1, followed by projects, a list of entries with id, name, path and added_at. A key qcrew does not know, or another schema version, is an error rather than being ignored.

The qcrew service is the only program that writes the file; the project commands ask it and start it on demand. It writes under a lock, keeps the file private to your user (mode 0600), and saves the replaced version in backups/ beside the file first. If you edit the file while the service saves it, the save fails with conflict and nothing is written; repeat the command. One service serves one configuration file: a command that names another file while the service runs fails with config-mismatch until you stop the service.`

const sessionTopic = `An agent session runs a tool in the directory of a project: claude (Claude Code), codex (Codex) or shell (your login shell). Sessions run in tmux, on a tmux server of qcrew's own named qcrew, so they keep running when the service stops or the terminal closes, and your own tmux server is never touched.

Commands:
- qcrew session start --project P --tool T [--title X]: start a session in the project with the id or name P; the title defaults to the tool
- qcrew session list [--project P]: show id, project, tool, title, status, since, context_percent and started_at of the sessions (--output table, json or compact)
- qcrew session stop <id> --confirm: end the session and close its window
- qcrew session attach <id>: print the tmux command that shows the session, and run it at a terminal; detach with the tmux prefix key and d, and the session keeps running

The status is running, waiting, idle, error or exited, and since is when the session took it. For claude, the hooks of Claude Code report it: running while it works, waiting for a permission or an answer, idle after it finished a turn, exited after the session ended, and error if its program ended without saying so or a hook sent something unreadable. qcrew starts claude with a settings file of its own for each session, below ~/.qatlas/qcrew/sessions, and changes neither your settings nor those of the project. context_percent is how full the context window is; qcrew learns it from a status line it adds, and leaves it empty if you have a status line of your own. For codex and shell, the status is running while the tool runs and exited after it ended. An exited session keeps its window, with the last output, until it is stopped.

Each project has one tmux session named p-<project id>, and each agent session is a window in it named s-<session id>. You may attach with tmux -L qcrew attach yourself and use tmux as usual.

Errors: dependency-missing when tmux is not installed and tool-missing when the tool is not in PATH; run qcrew setup. A project with running sessions cannot be removed (in-use).

QCREW_TMUX_SOCKET, an absolute path, moves qcrew to the tmux server at that socket instead; set it alike for the service and the commands.`
