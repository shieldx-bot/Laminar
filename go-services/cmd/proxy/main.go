package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"database/sql"
	pb "github/shieldx-bot/laminar/pb"

	wk "github/shieldx-bot/laminar/internal/worker"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq" // Driver postgres
	"golang.org/x/sync/singleflight"
	"google.golang.org/protobuf/encoding/protojson"
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

		// ==== Giữ lại logic gọi callback client (nếu có url) ====
		url := job.Req.GetUrlcallback()
		if strings.TrimSpace(url) == "" {
			continue
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "http://" + url
		}
		url = strings.TrimRight(url, "/") + "/callback-query-client"

		// Gửi request gốc (như code hiện tại). Nếu bạn muốn gửi cả "res" thì cần proto/endpoint hỗ trợ.
		body, mErr := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(job.Req)
		if mErr != nil {
			continue
		}

		req, rErr := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if rErr != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			continue
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		fmt.Printf("Sent request to callback client: %s, response status: %s, body: %s\n", url, resp.Status, strings.TrimSpace(string(b)))
	}
}

func init() {
	go StartJobToCallBack()
}

func main() {
	connStr := "host=34.177.108.132 port=5432 user=postgres password=Vananh12345@ dbname=laminar sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	defer db.Close()

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
			QueryId     string `json:"query_id"`
			QuerySQL    string `json:"query_sql"`
			Payload     string `json:"payload"`
			Urlcallback string `json:"url_callback"`
			Action      string `json:"action"`
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
			Ctx: c.Request.Context(),
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
	if port == "" {
		port = "8081"
	}
	fmt.Printf("Starting server on :%s\n", port)
	router.Run(":" + port)
	_ = myServer // giữ nếu bạn còn dùng nơi khác; nếu không dùng nữa có thể xoá
}
