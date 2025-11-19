package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HttpServer struct {
	router *gin.Engine
}

func (w *HttpServer) Start() {
	w.router = gin.Default()
	w.router.SetTrustedProxies(nil)

	w.router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	w.router.Run(":8888")
}
