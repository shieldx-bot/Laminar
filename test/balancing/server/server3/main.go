package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	server := gin.Default()
	server.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, "Pong from Server 1")
	})
	fmt.Println("Server 3 running on :8083")
	server.Run(":8083")
}
