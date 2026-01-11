package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize a Gin router with default middleware (logger and recovery)
	router := gin.Default()

	// Define a GET route for the "/ping" endpoint
	router.GET("/ping", func(c *gin.Context) {
		// Return a JSON response with HTTP status 200 (OK)
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	// Run the server on port 8080 (0.0.0.0:8080)
	router.Run(":8080")
}
