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
Also register one or more notifiers, with your custom logic describing how to react to a mention.

```go
receiver := webmention.NewReceiver(
  webmention.WithNotifier(
    // your custom handlers
    LogMentions,
    SaveMentionsToDB,
    NotifyOnMatrix,
    NotifyByEMail,
  ),
)

// goroutine asynchronously validates and processes received webmentions
// webmentions that pass validation are passed on to the listeners
go receiver.ProcessMentions()

http.HandleFunc("/api/webmention", receiver.WebmentionEndpoint) // register webmention endpoint
http.ListenAndServe(":8080", nil)
```

For a more comprehensive example, including how to cleanly shutdown the receiver, look at the [example implementation](cmd/mentionee/main.go).

Notifiers need to implement the `Notifier` interface, which defines a single `Receive` method.

```go
type MentionLogger struct{}
func (MentionLogger) Receive(mention webmention.Mention) {
  slog.Info("received mention", "mention", mention)
}
var LogMentions MentionLogger
```

## Run as a service

### Sending Webmentions

[Mentioner](cmd/mentioner/) can be run as a daemon to listen for commands on a socket.

```sh
cd cmd/mentioner
go build .
sudo cp mentioner /usr/local/bin/
sudo cp mentioner.service mentioner.socket /etc/systemd/system/
sudo systemctl start mentioner.socket
```

Something managing a source, eg., a blogging software, can send a command through the socket, instructing the Sender to send out webmentions.

```sh
socat - UNIX-CONNECT:/var/run/mentioner.socket
{"mentions":[{"source":"https://example.com/blog.html","past_targets":[],"current_targets":["https://example.com/some_other_blog.html"]}]}

```

A command has the following JSON structure:

```json
{
  "mentions": [
    {
      "source": "<source 1 url>",
      "past_targets": [
        "<target 1 url>",
        "<target 2 url>",
        "<target ... url>"
      ],
      "current_targets": [
        "<target 1 url>",
        "<target 2 url>",
        "<target ... url>"
      ]
    },
    {
      "source": "<source 2 url>",
      "past_targets": [],
      "current_targets": []
    }
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
  ],
  "error": ""
}
```

An empty error string indicates success.

### Receiving Webmentions

[Mentionee](cmd/mentionee/) is a daemon that listens to incoming Webmentions.

```sh
cd cmd/mentionee
go build .
sudo cp mentionee /usr/local/bin/
sudo cp mentionee.service /etc/systemd/system/
sudo systemctl start mentionee.service
```

Since it listens on a local port (per default :8080), you can configure your web server to forward requests to it.

```nginx
location = /api/webmention {
	proxy_pass http://localhost:8080;
	proxy_set_header Host $host;
	proxy_set_header X-Real-IP $remote_addr;
	proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

Don't forget to advertise your Webmention endpoint!

One way is by sending `Link` headers:

```nginx
location ~* \.html$ {
	expires 30d;
	add_header Cache-Control public;
	add_header Link "</api/webmention>; rel=webmention";
}
```

Another options is to add a `<link>` to your blog posts:

```html
<html lang="en">
    <head>
        <meta charset="utf-8">
        <link rel="webmention" href="/api/webmention"> <!-- << advertise webmention endpoint here << -->
  </head>
  <body>
    <!-- Some super exciting blog post... -->
  </body>
</html>
```
