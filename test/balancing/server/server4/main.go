package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	server := gin.Default()
	server.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, "Pong from Server 4")
	})
	fmt.Println("Server 4 running on :8084")
	server.Run(":8084")
}
