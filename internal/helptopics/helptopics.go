// Package helptopics holds the short guides that ship inside the binary. The command line prints them as help
// topics.
package helptopics

import "github.com/castrowithcee/qatlas-kit/helptopics"

// All returns the topics in the order a newcomer reads them. The text is plain ASCII with one line per
// paragraph, list item, or command; the kit fits it to the width where it is shown.
func All() []helptopics.Topic {
	return []helptopics.Topic{
		{Name: "qcrew", Short: "What qcrew is and what it is for", Text: overview},
	}
}

const overview = `qcrew is a program with a terminal interface for working with several AI agent command line tools, such as Claude Code and Codex, in parallel across several projects. It is in an early state: the terminal interface is not available yet.

Commands:
- qcrew version: print the version
- qcrew self-update [version], qcrew self-update status: update qcrew itself to the latest stable release or to a given newer one from its signed GitHub releases; status shows the installed version and the latest release and changes nothing (updating needs --confirm)
- qcrew help <topic>: show a help topic`
