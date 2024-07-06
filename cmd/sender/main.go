// Provides a service that will listen on an unix socket for commands.
// Received commands are parsed into a source and a list of (past and current) targets.
// The service then proceeds to send a mention from source to each of the targets.
//
// Intended use-case for this service is to be run as a daemon.
// A source, eg., a blogging engine can then contact this daemon through its socket.
// This way, every time a new blog post is compiled with the blogging software,
// the blogger can notify the daemon about any links mentioned in the post.
package main

import (
	webmention "github.com/cvanloo/gowebmention"
)

func main() {
	// open unix socket

	// listen on socket

	// process send request
	err := webmention.Update(source, targets...)

	// handle shutdown
}
