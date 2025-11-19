package ui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type WebServer struct {
	router *gin.Engine
}

func (w *WebServer) Start() {
	w.router = gin.Default()

	w.router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	w.router.Run()
}
