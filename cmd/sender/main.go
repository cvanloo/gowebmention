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
	"log/slog"
	"os"
	"encoding/json"
	"net/url"
	"io"
	"net"
	"errors"
	"fmt"

	webmention "github.com/cvanloo/gowebmention"
)

var sender webmention.WebMentionSender

func init() {
	sender = webmention.NewSender()
}

func must[T any](t T, err error) T {
	if err != nil {
		panic(err)
	}
	return t
}

func main() {
	// example request:
	// {"mentions":[{"source":"http://localhost:8080/hello.html","past_targets":[],"current_targets":["http://localhost:8080/bye.html"]}]}

	listener, err := net.Listen("unix", "/tmp/wmsend.sock") // @todo: configure socket, default somewhere in /var ?
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		go handle(conn)
	}

	// @todo: handle shutdown
}

type (
	URL struct {
		*url.URL
	}
	MentionsMessage struct {
		Mentions []Mention `json:"mentions"`
	}
	Mention struct {
		Source URL `json:"source"`
		PastTargets []URL `json:"past_targets"`
		CurrentTargets []URL `json:"current_targets"`
	}
	MentionsResponse struct {
		Statuses []Status `json:"statuses"`
		Error string `json:"error"`
	}
	Status struct {
		Source URL `json:"source"`
		Error string `json:"error"`
	}
)

func (u URL) MarshalJSON() ([]byte, error) {
	return []byte("\"" + u.URL.String() + "\""), nil
}

func (u *URL) UnmarshalJSON(bs []byte) error {
	if bs[0] != '"' || bs[len(bs)-1] != '"' {
		return fmt.Errorf("malformed url value: %s: needs to be enclosed in quotes", string(bs))
	}
	s := string(bs[1:len(bs)-1])
	url, err := url.Parse(s)
	u.URL = url
	return err
}

type (
	ConnError struct{
		error
	}
	UserError struct{
		error
	}
)

func handle(conn net.Conn) {
	//conn.SetDeadline(time.Now().Add(20*time.Second)) // @todo: idle timeout?
	defer func() {
		err := conn.Close()
		if err != nil {
			slog.Error(err.Error(), "remote", conn.RemoteAddr())
		}
	}()

	err := handleRequest(conn)
	if err == nil {
		return
	}

	var connErr ConnError
	if errors.As(err, &connErr) {
		slog.Error(connErr.Error())
		return
	}

	var userErr UserError
	if errors.As(err, &userErr) {
		slog.Error(userErr.Error())
		statuses := MentionsResponse{
			Error: userErr.Error(),
		}
		resp, err := json.Marshal(statuses)
		if err != nil {
			slog.Error(err.Error())
			return
		}
		if _, err := conn.Write(resp); err != nil {
			slog.Error(err.Error())
			return
		}
		return
	}
}

func handleRequest(conn net.Conn) error {
	// @todo: instead of readall read till newline
	// so the connection can be kept open to receive more commands
	// - also add idle timeout, close connection if no commands were received
	// in a certain time
	// - and add connection limit?
	bs, err := io.ReadAll(conn)
	if err != nil {
		return ConnError{err}
	}

	var mentions MentionsMessage
	err = json.Unmarshal(bs, &mentions)
	if err != nil {
		return UserError{err}
	}

	var statuses MentionsResponse
	for _, mention := range mentions.Mentions {

		// Holy 💩, the Go type system sucks, and it sucks hard!!!
		pastTargets := make([]*url.URL, len(mention.PastTargets))
		for i, target := range mention.PastTargets {
			pastTargets[i] = target.URL
		}
		currentTargets := make([]*url.URL, len(mention.CurrentTargets))
		for i, target := range mention.CurrentTargets {
			currentTargets[i] = target.URL
		}

		err := sender.Update(mention.Source.URL, pastTargets, currentTargets)
		status := Status{
			Source: mention.Source,
		}
		if err != nil {
			status.Error = err.Error()
		}
		statuses.Statuses = append(statuses.Statuses, status)
	}

	resp, err := json.Marshal(statuses)
	if err != nil {
		return UserError{err}
	}
	_, err = conn.Write(resp)
	if err != nil {
		return ConnError{err}
	}

	return nil
}
