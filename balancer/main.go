package main

import (
	"fmt"
	wk "github/shieldx-bot/loadbanlacing/internal/worker"
	"github/shieldx-bot/loadbanlacing/pb"
	"net/http"

	"github.com/gin-gonic/gin"
)

type server struct {
	pb.UnimplementedLaminarGatewayServer
	cs *wk.ComputeServer
}

func NewServer(cs *wk.ComputeServer) *server {
	return &server{
		cs: cs,
	}
}

func main() {
	computeServer := wk.NewComputeServer()
	_ = computeServer
	Myserver := NewServer(computeServer)
	_ = Myserver

	router := gin.Default()

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, "Pong from Load Balancer")
	})

	router.POST("/balance", func(c *gin.Context) {
		var req pb.RequestToBalancer
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		res, err := computeServer.SubmitJob(c.Request.Context(), &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, res)
	})

	fmt.Println("Load Balancer running on :8089")
	router.Run(":8089")
}
