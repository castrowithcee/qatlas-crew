// Package setup finds the programs qcrew needs, tmux, Claude Code and Codex, and installs missing ones by
// the way their makers document. It never runs a command by itself: Install shows each command in full and
// asks first, asks again before every sudo command, and refuses without a terminal.
package setup
