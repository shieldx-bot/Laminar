package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"database/sql"
	pb "github/shieldx-bot/laminar/pb"

	wk "github/shieldx-bot/laminar/internal/worker"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq" // Driver postgres
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

var requestCoalescer singleflight.Group

// NEW: để StartJobToCallBack() gọi được ExecuteQuery
var globalComputeServer *wk.ComputeServer

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

// NEW: result để /TestHTTP3 có thể đợi kết quả từ worker
type JobCallBackResult struct {
	Resp *pb.CallBackResponse
	Err  error
}

type JobCallBack struct {
	Ctx      context.Context
	Key      string
	Req      *pb.CallBackRequest
	RespChan chan JobCallBackResult // nil nếu fire-and-forget
}

var JobCallBackChan chan *JobCallBack = make(chan *JobCallBack, 1000)

func StartJobToCallBack() {
	for job := range JobCallBackChan {
		if job == nil || job.Req == nil {
			continue
		}
		if globalComputeServer == nil {
			if job.RespChan != nil {
				job.RespChan <- JobCallBackResult{Err: fmt.Errorf("compute server not initialized")}
			}
			continue
		}

		key := job.Key
		if key == "" {
			key = job.Req.GetQuerySQL()
			if key == "" {
				key = job.Req.GetQueryId()
			}
		}

		// ==== MOVED FROM /TestHTTP3: singleflight + timeout + ExecuteQuery ====
		resAny, err, _ := requestCoalescer.Do(key, func() (interface{}, error) {
			baseCtx := job.Ctx
			if baseCtx == nil {
				baseCtx = context.Background()
			}
			ctx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
			defer cancel()
			return globalComputeServer.ExecuteQuery(ctx, job.Req)
		})

		var res *pb.CallBackResponse
		if err == nil && resAny != nil {
			res = resAny.(*pb.CallBackResponse)
		}

		// Trả kết quả về handler nếu cần
		if job.RespChan != nil {
			job.RespChan <- JobCallBackResult{Resp: res, Err: err}
		}
		var redisHost = os.Getenv("LAMINAR_REDIS_HOST")
		var redisPort = os.Getenv("LAMINAR_REDIS_PORT")
		rdb := redis.NewClient(&redis.Options{Addr: redisHost + ":" + redisPort})
		ctx := context.Background()
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := rdb.Publish(ctx, "query_done", b).Err(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Callback Result for Key=%s: err=%v, resp=%s\n", key, err, string(b))

	}
}

func init() {
	go StartJobToCallBack()
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}
	var host_database = os.Getenv("LAMINAR_DB_HOST")
	var port_database = os.Getenv("LAMINAR_DB_PORT")
	var user_database = os.Getenv("LAMINAR_DB_USER")
	var password_database = os.Getenv("LAMINAR_DB_PASSWORD")
	var name_database = os.Getenv("LAMINAR_DB_NAME")

	fmt.Println("Starting Laminar Proxy Server...")
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", host_database, port_database, user_database, password_database, name_database)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	//
	db.SetMaxOpenConns(200)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		fmt.Println("DB Fail:", err)
	} else {
		fmt.Println("Connected to DB successfully")
	}

	// 2.5 KHỞI TẠO COMPUTE SERVER (WORKER POOL) MỘT LẦN
	computeServer := wk.NewComputeServer(db)

	// NEW: gán cho worker dùng
	globalComputeServer = computeServer

	myServer := NewServer(db, computeServer)

	router := gin.Default()

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})

	router.POST("/router-backend", func(c *gin.Context) {
		var json struct {
			QueryId     string `json:"QueryId"`
			QuerySQL    string `json:"QuerySQL"`
			Payload     string `json:"Payload"`
			Urlcallback string `json:"Urlcallback"`
			Action      string `json:"Action"`
		}

		if err := c.BindJSON(&json); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		key := json.QuerySQL
		if key == "" {
			key = json.QueryId
		}

		select {
		case JobCallBackChan <- &JobCallBack{
			// Fix: Dùng context.Background() thay vì c.Request.Context()
			// Vì request kết thúc ngay (fire-and-forget) nên Context của nó sẽ bị Cancel -> Worker bị lỗi context canceled
			Ctx: context.Background(),
			Key: key,
			Req: &pb.CallBackRequest{
				QueryId:     json.QueryId,
				QuerySQL:    json.QuerySQL,
				Payload:     []byte(json.Payload),
				Urlcallback: json.Urlcallback,
				Action:      json.Action,
			},
			RespChan: nil, // fire-and-forget
		}:
		case <-time.After(2 * time.Second):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Server busy, try again later"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "received"})
	})

	port := os.Getenv("LAMINAR_PROXY_PORT")
	// if port == "" {
	// 	port = "8081"
	// }
	fmt.Printf("Starting server on :%s\n", port)
	router.Run(":" + port)
	_ = myServer // giữ nếu bạn còn dùng nơi khác; nếu không dùng nữa có thể xoá
}
