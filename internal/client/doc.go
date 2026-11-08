// Package client is how the command line and the terminal interface call the service: over its local
// channel and nowhere else.
//
// Dial checks, through the channel, that the listening process runs as the same user from the same program
// file. A call sends one operation and returns its result; an error answer of the service becomes a
// *cli.Failure with a code of the shared set, so a command can return it as it is.
package client
