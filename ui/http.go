// Package ui provides HTTP server functionality for the Grid Trading Bot.
package ui

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// HTTPServer represents the HTTP server for the bot.
type HTTPServer struct {
	router *gin.Engine
}

// Start initializes and starts the HTTP server on the specified port.
func (w *HTTPServer) Start(port int) error {
	w.router = gin.Default()

	// SetTrustedProxies with nil means trust no proxies, error is unlikely
	//nolint:errcheck // nil input cannot fail
	_ = w.router.SetTrustedProxies(nil)

	w.router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	// Run starts the server and blocks until error occurs
	// Common errors: port already in use, permission denied
	addr := fmt.Sprintf(":%d", port)
	if err := w.router.Run(addr); err != nil {
		return err
	}

	return nil
}
