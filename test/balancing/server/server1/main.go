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
	server.POST("/router-backend", func(c *gin.Context) {
		var json struct {
			QueryId     string `json:"query_id"`
			QuerySQL    string `json:"query_sql"`
			Payload     string `json:"payload"`
			Urlcallback string `json:"url_callback"`
			Action      string `json:"action"`
		}
		fmt.Printf("Đã nhận request từ load banlancing - WebHook: %s", json.Urlcallback)
		if err := c.BindJSON(&json); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "received",		
		})
	})
	fmt.Println("Server 1 running on :8081")
	server.Run(":8081")
}
