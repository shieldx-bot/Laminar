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

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq" // Driver postgres
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var testHTTP3SingleFlight singleflight.Group
var queryCache *ristretto.Cache

func main() {

	router := gin.Default()

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	})
	if err != nil {
		panic(fmt.Errorf("failed to create ristretto cache: %w", err))
	}
	queryCache = cache

	grpcAddr := os.Getenv("LAMINAR_GRPC_ADDR")
	if grpcAddr == "" {
		grpcAddr = "34.87.152.48:50051"
	}
	grpcConn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Errorf("dial %s: %w", grpcAddr, err))
	}
	defer grpcConn.Close()
	grpcClient := pb.NewLaminarGatewayClient(grpcConn)

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

		key := jsonReq.QuerySQL
		if key == "" {
			key = jsonReq.QueryId
		}

		// 1) Local cache at gateway (hot responses)
		if val, ok := queryCache.Get(key); ok {
			if cachedResp, ok := val.(*pb.TestHTTP3Response); ok {
				c.JSON(http.StatusOK, gin.H{
					"Status":       cachedResp.GetStatus(),
					"QueryId":      jsonReq.QueryId,
					"Records":      cachedResp.GetRecords(),
					"ReceivedSize": cachedResp.GetReceivedSize(),
				})
				return
			}
		}

		resAny, err, _ := testHTTP3SingleFlight.Do(key, func() (interface{}, error) {
			// Double-check cache inside singleflight to avoid duplicate work
			if val, ok := queryCache.Get(key); ok {
				if cachedResp, ok := val.(*pb.TestHTTP3Response); ok {
					return cachedResp, nil
				}
			}

			ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
			defer cancel()
			payload := make([]byte, 10)
			resp, err := grpcClient.TestHTTP3(ctx, &pb.TestHTTP3Request{
				QueryId:  jsonReq.QueryId,
				QuerySQL: jsonReq.QuerySQL,
				Payload:  payload,
			})
			if err != nil {
				return nil, err
			}
			// 2) Store into gateway cache (TTL 5s)
			queryCache.SetWithTTL(key, resp, 1, 5*time.Second)
			return resp, nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("TestHTTP3: %v", err)})
			return
		}

		resp := resAny.(*pb.TestHTTP3Response)
		// Preserve per-request QueryId even when coalesced.
		c.JSON(http.StatusOK, gin.H{
			"Status":       resp.GetStatus(),
			"QueryId":      jsonReq.QueryId,
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
