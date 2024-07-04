// https://webmention.net/draft/#sending-webmentions-for-updated-posts-p-4
package webmention

import (
	"fmt"
	"log/slog"
	"os"
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

func Run() {
	fmt.Println("Hello, Webmention!")
}
