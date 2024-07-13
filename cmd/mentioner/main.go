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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	webmention "github.com/cvanloo/gowebmention"
)

var sender webmention.WebMentionSender

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
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

	// cd cmd/mentioner/
	// go build .
	// sudo cp mentioner /usr/local/bin/mentioner
	// sudo cp mentioner.service mentioner.socket /etc/systemd/system/
	// sudo systemctl start mentioner.socket
	// socat - UNIX-CONNECT:/var/run/mentioner.socket

	fd := os.NewFile(3, "mentioner.socket")
	listener, err := net.FileListener(fd)
	if err != nil {
		slog.Info("no valid socket passed as fd=3, creating /tmp/mentioner.socket instead")
		l, err := net.Listen("unix", "/tmp/mentioner.socket")
		if err != nil {
			slog.Error(err.Error())
			os.Exit(1)
		}
		listener = l
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					slog.Info("listener closed: stopped accepting connections")
				} else {
					slog.Error(err.Error())
				}
				return
			}
			go handle(conn) // @todo: max number of open connections?
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	<-c // wait for interrupt
	slog.Info("interrupt received: shutting down")

	if err := listener.Close(); err != nil {
		slog.Error(err.Error())
	}
}

type (
	URL struct {
		*url.URL
	}
	MentionsMessage struct {
		Mentions []Mention `json:"mentions"`
	}
	Mention struct {
		Source         URL   `json:"source"`
		PastTargets    []URL `json:"past_targets"`
		CurrentTargets []URL `json:"current_targets"`
	}
	MentionsResponse struct {
		Statuses []Status `json:"statuses"`
		Error    string   `json:"error"`
	}
	Status struct {
		Source URL    `json:"source"`
		Error  string `json:"error"`
	}
)

func (u URL) MarshalJSON() ([]byte, error) {
	return []byte("\"" + u.URL.String() + "\""), nil
}

func (u *URL) UnmarshalJSON(bs []byte) error {
	if bs[0] != '"' || bs[len(bs)-1] != '"' {
		return fmt.Errorf("malformed url value: %s: needs to be enclosed in quotes", string(bs))
	}
	s := string(bs[1 : len(bs)-1])
	url, err := url.Parse(s)
	u.URL = url
	return err
}

type MessageError error

func handle(conn net.Conn) {
	//conn.SetDeadline(time.Now().Add(20*time.Second)) // @todo: idle timeout?
	defer func() {
		err := conn.Close()
		if err != nil {
			slog.Error("closing connection", "connection_error", err.Error(), "remote", conn.RemoteAddr())
		}
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		statuses, err := handleRequest(scanner.Bytes())

		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return // stop handler for this (closed) socket
			}

			var msgErr MessageError
			if errors.As(err, &msgErr) {
				statuses.Error = msgErr.Error()
			}
		}

		resp, err := json.Marshal(statuses)
		if err != nil {
			slog.Error("cannot marshal statuses response", "marshal_error", err.Error(), "statuses", statuses)
			return // close connection, stop handler
		}
		if _, err := conn.Write(resp); err != nil {
			return // connection was probably closed, stop handler
		}
	}
}

func handleRequest(message []byte) (resp MentionsResponse, err error) {
	if len(message) == 0 {
		return resp, MessageError(fmt.Errorf("boredom: you didn't give me anything to do"))
	}
	var mentions MentionsMessage
	if err := json.Unmarshal(message, &mentions); err != nil {
		return resp, MessageError(fmt.Errorf("invalid message: %w", err))
	}
	if len(mentions.Mentions) == 0 {
		return resp, MessageError(fmt.Errorf("boredom: you didn't give me anything to do"))
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

	return statuses, nil
}
