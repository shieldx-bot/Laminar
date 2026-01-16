package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	server := gin.Default()
	server.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, "Pong from Server 2")
	})
	fmt.Println("Server 1 running on :8082")
	server.Run(":8082")
}
