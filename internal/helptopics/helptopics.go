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
	}
}

const overview = `qcrew is a program with a terminal interface for working with several AI agent command line tools, such as Claude Code and Codex, in parallel across several projects. It is in an early state: the terminal interface is not available yet.

Commands:
- qcrew version: print the version
- qcrew service start|stop|status: control the background service, see the service-guide topic
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
