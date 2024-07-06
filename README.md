# Webmention library and service implementation in Go

This package can be used either as a standalone application or included as a library in your own projects.

```sh
go get github.com/cvanloo/gowebmention
```

Import it:

```go
import webmention "github.com/cvanloo/gowebmention"
```

## Use as a library

Sending webmentions can be done through a `WebMentionSender`.

```go
sender := webmention.NewSender()
sender.Update(source, pastMentions, currentMentions)
// source: the url for which you want to send mentions
// pastMentions: if you have sent mentions for the same url before, this list should include all targets mentioned the last time
//               otherwise you can leave the list empty or nil
// currentMentions: all targets that the source is currently mentioning
```

If you are sending updates for a now deleted source, it is your responsibility to ensure that the source is returning 410 Gone,
optionally returning a tombstone representation of the old source as the response body.

Also note that the library does not persist anything.
It is on you to remember `pastMentions`.

To receive webmentions setup an http endpoint and get the processing goroutine going.
Also register one or more listeners, with your custom logic describing how to react to a mention.

```go
receiver := webmention.NewReceiver(
  webmention.WithListener(
    // your custom handlers
    LogMentions,
    SaveMentionsToDB,
    NotifyOnMatrix,
  ),
)

// goroutine asynchronously validates and processes received webmentions
// webmentions that pass validation are passed on to the listeners
go receiver.ProcessMentions()

http.HandleFunc("/webmention", receiver.WebmentionEndpoint) // register webmention endpoint
http.ListenAndServe(":8080", nil)
```

For a more comprehensive example, including how to cleanly shutdown the receiver, look at the [example implementation](cmd/receiver/main.go).

Listeners need to implement the `Listener` interface, which defines a single `Receive` method.

```go
type MentionLogger struct{}
func (MentionLogger) Receive(mention webmention.IncomingMention, rel webmention.Relationship) {
  slog.Info("received mention", "source", mention.Source.String(), "target", mention.Target.String(), "rel", rel)
}
var LogMentions MentionLogger
```

## Run as a service

[Sender](cmd/sender/) can be run as a daemon to listen for commands on a socket.

Something managing a source, eg., a blogging software, can send a command through the socket, instructing the Sender to send out webmentions.

A command has the following JSON structure:

```json
{
  "mentions": [
    {
      "source": "<source 1 url>",
      "past_targets": [
        "<target 1 url>",
        "<target 2 url>",
        ...
      ],
      "current_targets": [
        "<target 1 url>",
        "<target 2 url>",
        ...
      ]
    },
    {
      "source": "<source 2 url>",
      "past_targets": [...],
      "current_targets": [...],
    },
    ...
  ]
}
```

For each of the sources, the past and current targets will be mentioned.

The daemon responds for each mention with whether it was successful or not:

```json
{
  "statuses": [
    {
      "source": "<source 1 url>",
      "error": ""
    }
    ...
  ],
  "error": ""
}
```

An empty error string indicates success.

TODO: Finish implementing logic, then write this section.
