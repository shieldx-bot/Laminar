package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github/shieldx-bot/gateway/pb"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq" // Driver postgres
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {

	router := gin.Default()

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Fast endpoint for QUIC multiplexing tests (small response)
	router.GET("/fast", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.GET("/ping-service-go", func(c *gin.Context) {
		fetchURL := "http://34.87.152.48:8081/api/ping" // Thay đổi URL theo yêu cầu
		resp, err := http.Get(fetchURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch from external host"})
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode response"})
			return
		}

		c.JSON(http.StatusOK, result)
	})

	router.POST("/TestHTTP3-service-go", func(c *gin.Context) {
		var jsonReq struct {
			QueryId  string `json:"QueryId"`
			QuerySQL string `json:"QuerySQL"`
			Payload  string `json:"Payload"`
		}
		if err := c.BindJSON(&jsonReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		addr := "http://34.87.152.48:8081/api/naive"
		payload := map[string]string{
			"QueryId":  jsonReq.QueryId,
			"QuerySQL": jsonReq.QuerySQL,
			"Payload":  jsonReq.Payload,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal payload"})
			return
		}

		resp, err := http.Post(addr, "application/json", bytes.NewBuffer(payloadBytes))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send request to external host"})
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode response"})
			return
		}

		c.JSON(http.StatusOK, result)
	})
	// POST query endpoint (good for load tests; avoids any accidental intermediary caching)
	router.POST("/TestHTTP3", func(c *gin.Context) {
		var jsonReq struct {
			QueryId  string `json:"QueryId"`
			QuerySQL string `json:"QuerySQL"`
			Payload  string `json:"Payload"`
		}
		if err := c.BindJSON(&jsonReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		addr := "34.87.152.48:50051"

		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("dial %s: %v", addr, err)})
			return
		}
		defer conn.Close()

		client := pb.NewLaminarGatewayClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		Payload := make([]byte, 10) // Example payload (1024 bytes)

		resp, err := client.TestHTTP3(ctx, &pb.TestHTTP3Request{
			QueryId:  jsonReq.QueryId,
			QuerySQL: jsonReq.QuerySQL,
			Payload:  Payload,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("TestHTTP3: %v", err)})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"Status":       resp.GetStatus(),
			"QueryId":      resp.GetQueryId(),
			"Records":      resp.GetRecords(),
			"ReceivedSize": resp.GetReceivedSize(),
		})

	})

	port := os.Getenv("LAMINAR_PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	fmt.Printf("Starting server on :%s\n", port)

	router.Run("0.0.0.0:" + port) // listen and serve
}
