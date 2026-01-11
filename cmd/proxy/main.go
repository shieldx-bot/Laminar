package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"database/sql"
	pb "github/shieldx-bot/laminar/pb"

	wk "github/shieldx-bot/laminar/internal/worker"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq" // Driver postgres
)

type server struct {
	pb.UnimplementedLaminarGatewayServer
	db *sql.DB // 1. Thêm field này để tái sử dụng DB Pool
	cs *wk.ComputeServer
}

// Hàm khởi tạo Server mới, nhận DB từ bên ngoài vào
func NewServer(db *sql.DB, cs *wk.ComputeServer) *server {
	return &server{
		db: db,
		cs: cs,
	}
}
func main() {
	connStr := "host=34.177.108.132 port=5432 user=postgres password=Vananh12345@ dbname=laminar sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	// Đừng đóng DB ngay, chỉ đóng khi main exit
	defer db.Close()

	// Cấu hình Connection Pool (Quan trọng cho High Performance)
	db.SetMaxOpenConns(200)  // Giới hạn max 1000 kết nối cùng lúc
	db.SetMaxIdleConns(25)   // Giữ 25 kết nối rảnh để dùng ngay
	db.SetConnMaxLifetime(0) // 0 = dùng mãi mãi (hoặc set time để refresh)

	// Ping kiểm tra
	if err := db.Ping(); err != nil {
		fmt.Println("DB Fail:", err)
		// Có thể return hoặc panic tùy chiến lược
	} else {
		fmt.Println("Connected to DB successfully")
	}

	// 2.5 KHỞI TẠO COMPUTE SERVER (WORKER POOL) MỘT LẦN
	computeServer := wk.NewComputeServer(db)

	// HTTP proxy/gateway for benchmarking (can be placed behind Nginx HTTP/3)
	myServer := NewServer(db, computeServer)

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

		pbReq := &pb.TestHTTP3Request{
			QueryId:  jsonReq.QueryId,
			QuerySQL: jsonReq.QuerySQL,
			Payload:  []byte(jsonReq.Payload),
		}
		res, err := myServer.cs.ExecuteQuery(context.Background(), pbReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"status":        res.Status,
			"query_id":      res.QueryId,
			"received_size": res.ReceivedSize,
			"record_count":  len(res.Records),
		})
	})

	// GET user endpoint (more REST-like for read-heavy benchmarks)
	// Example: GET /user/123 or GET /user?id=123
	router.GET("/user", func(c *gin.Context) {
		idStr := c.Query("id")
		if idStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing id"})
			return
		}
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		pbReq := &pb.TestHTTP3Request{
			QueryId:  fmt.Sprintf("req_%d", id),
			QuerySQL: fmt.Sprintf("SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users WHERE id = %d", id),
		}
		res, err := myServer.cs.ExecuteQuery(context.Background(), pbReq)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{
			"status":        res.Status,
			"query_id":      res.QueryId,
			"received_size": res.ReceivedSize,
			"record_count":  len(res.Records),
		})
	})

	router.POST("/TestHTTP3_no_backend", func(c *gin.Context) {
		var jsonReq struct {
			QueryId  string `json:"query_id"`
			QuerySQL string `json:"query_sql"`
			Payload  string `json:"payload"`
		}
		if err := c.BindJSON(&jsonReq); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		pbReq := &pb.TestHTTP3Request{
			QueryId:  jsonReq.QueryId,
			QuerySQL: jsonReq.QuerySQL,
			Payload:  []byte(jsonReq.Payload),
		}

		// Placeholder: no backend client wired yet; just acknowledge receipt.
		fmt.Printf("Request Test HTTP3 received (query_id=%s)\n", pbReq.QueryId)
		c.JSON(200, gin.H{"status": "ok"})
	})
	port := os.Getenv("LAMINAR_PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	fmt.Printf("Starting server on :%s\n", port)
	router.Run(":" + port) // listen and serve
}
