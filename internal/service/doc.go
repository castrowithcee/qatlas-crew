// Package service is the long-running part of qcrew that answers the client.
//
// One service runs per host and user, in the user's context and detached from the terminal that started it.
// It listens on a local channel that admits only processes of the same user running the same program file,
// reads one request per line (see package control), hands it to the handler of its operation and answers.
// It never ends for lack of use. It ends on SIGINT or SIGTERM, on the stop operation, or when the program
// file it runs from was replaced or removed. Its messages go to a log file below the qcrew data directory.
//
// The agent sessions run in tmux (see package session) and outlive the service. The service mirrors them
// from tmux on every request about them and every few seconds, and refuses to remove a project while
// sessions of it run.
package service
