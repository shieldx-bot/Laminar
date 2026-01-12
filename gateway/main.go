package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github/shieldx-bot/gateway/metrix"
	"github/shieldx-bot/gateway/pb"
	"io"
	"net/http"
	"os"
	"runtime"
	"sync/atomic"
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

type Job struct {
	Ctx    context.Context
	Metrix map[string]interface{}
}

var (
	jobChan = make(chan *Job, 1000)
	store   = make([]map[string]interface{}, 0, 10000)
)
var TotalOnQueue int64 = 0

func startWorker() {
	go func() {
		for job := range jobChan {
			store = append(store, job.Metrix)
			time.Sleep(5000 * time.Millisecond) // Simulate processing time

			body, err := json.Marshal(job.Metrix)
			if err != nil {
				continue
			}

			req, err := http.NewRequest(http.MethodPost, "http://localhost:8083/receive-metrics", bytes.NewReader(body))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
		}
	}()
}

func main() {
	startWorker()

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
		grpcAddr = "34.177.108.132:50051"
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
		fetchURL := "http://34.177.108.132:8081/api/ping" // Thay đổi URL theo yêu cầu
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
		addr := "http://34.177.108.132:8081/api/naive"
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
			if cachedBytes, ok := val.([]byte); ok {
				c.Data(http.StatusOK, "application/json", cachedBytes)
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
			buf, _ := json.Marshal(resp)
			// 2) Store into gateway cache (TTL 20s)
			queryCache.SetWithTTL(key, buf, 1, 20*time.Second)
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
	router.POST("/http3-proxy", func(c *gin.Context) {
		var reqBody map[string]interface{}
		var NumberTask int64
		MetrixFirst := metrix.MetrixFirstFunction()
		if err := c.BindJSON(&reqBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}
		atomic.AddInt64(&TotalOnQueue, 1)
		defer atomic.AddInt64(&TotalOnQueue, -1)

		// Mô phỏng logic xử lý công việc với các tác vụ khác nhau

		// Task 1: Cache HIT
		TimeStartTask1 := time.Now()
		_ = TimeStartTask1
		time.Sleep(100 * time.Millisecond)
		TimeEndTask1 := time.Now()
		TimeDoneTask1 := TimeEndTask1.Sub(TimeStartTask1)
		_ = TimeDoneTask1
		NumberTask += 1

		// Task 2: Cache MISS - Simulate longer processing
		TimeStartTask2 := time.Now()
		_ = TimeStartTask2
		time.Sleep(500 * time.Millisecond)
		TimeEndTask2 := time.Now()
		TimeDoneTask2 := TimeEndTask2.Sub(TimeStartTask2)
		_ = TimeDoneTask2
		NumberTask += 1

		// Cache MISS – follower
		TimeStartTask3 := time.Now()
		_ = TimeStartTask3
		time.Sleep(300 * time.Millisecond)
		TimeEndTask3 := time.Now()
		TimeDoneTask3 := TimeEndTask3.Sub(TimeStartTask3)
		_ = TimeDoneTask3
		NumberTask += 1

		TotalTimeTask := TimeDoneTask1.Milliseconds() + TimeDoneTask2.Milliseconds() + TimeDoneTask3.Milliseconds()

		MetrixEnd := metrix.MetrixEndFunction()
		cores := runtime.NumCPU()
		job := &Job{
			Ctx: c.Request.Context(),
			Metrix: map[string]interface{}{
				"TimeStartSend": time.Now().UnixNano() / int64(time.Millisecond),
				"TimeDoneTask":  MetrixEnd.TimeEnd - int64(MetrixFirst.TimeStart),
				"Penumj":        cores,
				"Pemips":        MetrixEnd.Pemips,
				"NumberTask":    NumberTask,
				"TTj":           MetrixEnd.TTj,
				"TLi":           TotalTimeTask * (NumberTask * int64(MetrixEnd.Pemips)),
				"IPVM":          c.ClientIP(),
				"TotalOnQueue":  atomic.LoadInt64(&TotalOnQueue),
				"IFS":           MetrixEnd.IFS,
				"VMbw":          MetrixEnd.VMbw,
			},
		}
		select {
		case jobChan <- job:
			c.JSON(http.StatusOK, gin.H{"status": "queued", "total_jobs": len(store)})
			return
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Server busy"})
		}

	})

	router.POST("/echo", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		time.Sleep(10 * time.Millisecond) // giả lập xử lý
		c.Data(200, "application/json", body)
	})

	port := os.Getenv("LAMINAR_PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	fmt.Printf("Starting server on :%s\n", port)

	router.Run("0.0.0.0:" + port) // listen and serve
}
