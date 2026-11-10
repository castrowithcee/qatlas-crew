// Package term shows a program running in a pseudo-terminal as a live terminal image inside the terminal
// interface. It starts a command in its own PTY, feeds its output through a VT parser and keeps the screen
// with colors, attributes and cursor. The caller draws that screen into any rectangle and forwards its size.
//
// The package never builds the command itself. In qcrew the command is the attach client of a tmux window;
// closing a [Term] ends that client only, never the session behind it.
package term
