// Package control holds the wire protocol between the client and the service and imports only the standard
// library.
//
// A message is one JSON object on one line of at most MaxMessage bytes. A request is
// {"v":1,"op":"<name>","args":{...}}; the answer is {"ok":true,"result":...} or
// {"ok":false,"code":"<code>","message":"..."}. Whatever arrives on the channel is data: a message is read
// with a size limit, and an unknown version or operation gets an error answer, never a crash.
package control
