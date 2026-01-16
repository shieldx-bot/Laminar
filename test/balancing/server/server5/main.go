package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	server := gin.Default()
	server.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, "Pong from Server 5")
	})
	fmt.Println("Server 5 running on :8085")
	server.Run(":8085")
}
