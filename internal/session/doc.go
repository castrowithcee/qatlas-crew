// Package session runs the agent sessions of the projects in tmux.
//
// Sessions live on a tmux server of their own, the named server qcrew, so that they outlive the terminal
// interface, the service and the terminal, and so that the user's own tmux server is never touched: every
// tmux call of this package names that server. Each project has one tmux session, p-<project id>, and each
// agent session is one window in it, s-<session id>, whose window options @qcrew-* carry the session's id,
// project, tool, title and start time. tmux is the truth about which sessions exist; a session whose
// program has ended stays as an exited window until it is stopped.
package session
