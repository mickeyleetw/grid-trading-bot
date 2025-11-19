package main

import (
	"github.com/mickeyleetw/grid-trading-bot/ui"
)

func main() {
	server := &ui.HttpServer{}
	server.Start()
}
