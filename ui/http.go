package ui

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type HTTPServer struct {
	router *gin.Engine
}

func (w *HTTPServer) Start(port int) error {
	w.router = gin.Default()
	_ = w.router.SetTrustedProxies(nil)

	w.router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	addr := fmt.Sprintf(":%d", port)
	if err := w.router.Run(addr); err != nil {
		return err
	}

	return nil
}
