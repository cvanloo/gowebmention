package listener

import (
	"log/slog"
	"context"
	"fmt"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
	webmention "github.com/cvanloo/gowebmention"
)

type MatrixBot struct{
	Client *mautrix.Client
	ReportToRoom id.RoomID
	FormatMessage func(webmention.Mention) string
}

func NewMatrixBot(client *mautrix.Client, reportToRoom id.RoomID) MatrixBot {
	return MatrixBot{
		Client: client,
		ReportToRoom: reportToRoom,
		FormatMessage: func(mention webmention.Mention) string {
			return fmt.Sprintf("Mention received!\nsource: %s\ntarget: %s\nstatus: %s\n", mention.Source, mention.Target, mention.Status)
		},
	}
}

func (bot MatrixBot) Receive(mention webmention.Mention) {
	resp, err := bot.Client.SendText(context.Background(), bot.ReportToRoom, bot.FormatMessage(mention))
	slog.Info("send text", "resp", resp)
	if err != nil {
		slog.Error("send text", "err", err)
	}
}
