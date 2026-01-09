package main

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"runtime"

	"github.com/dgraph-io/ristretto"
	_ "github.com/lib/pq"

	pb "github.com/shieldx-bot/laminar/pb"
)

type Job struct {
	Ctx      context.Context
	QueryId  string
	Action   string
	CT       *pb.TestHTTP3Request
	RespChan chan *JobResult
}

type JobResult struct {
	Resp *pb.TestHTTP3Response
	Err  error
}

type ComputeServer struct {
	pb.UnimplementedLaminarGatewayServer
	workerChans []chan *Job
	numShards   int
	cache       *ristretto.Cache
}

type ExampleRecord struct {
	ID            int    `json:"id"`
	USERNAME      string `json:"username"`
	EMAIL         string `json:"email"`
	PASSWORD_HASH string `json:"password_hash"`
	BALANCE       int64  `json:"balance"`
	IS_ACTIVE     bool   `json:"is_active"`
	CREATED_AT    string `json:"created_at"`
	UPDATED_AT    string `json:"updated_at"`
}

func ExecuteSQLQery(query string, db *sql.DB) ([]map[string]interface{}, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var record []ExampleRecord
	for rows.Next() {
		var r ExampleRecord
		if err := rows.Scan(&r.ID, &r.USERNAME, &r.EMAIL, &r.PASSWORD_HASH, &r.BALANCE, &r.IS_ACTIVE, &r.CREATED_AT, &r.UPDATED_AT); err != nil {
			return nil, err
		}
		record = append(record, r)

	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for _, r := range record {
		rowMap := map[string]interface{}{
			"id":            r.ID,
			"username":      r.USERNAME,
			"email":         r.EMAIL,
			"password_hash": r.PASSWORD_HASH,
			"balance":       r.BALANCE,
			"is_active":     r.IS_ACTIVE,
			"created_at":    r.CREATED_AT,
			"updated_at":    r.UPDATED_AT,
		}
		results = append(results, rowMap)
	}

	return results, nil

}
func NewComputeServer() *ComputeServer {
	numShares := runtime.NumCPU()

	cache, err := ristretto.NewCache(&ristretto.Config{
		// NumCounters: Số lượng keys ước tính (để tối ưu Bloom Filter).
		// Nên đặt gấp 10 lần số lượng key thực tế mong muốn.
		NumCounters: 1e7,
		// MaxCost: Giới hạn bộ nhớ tối đa (ví dụ 1GB).
		// Nếu vượt quá, Ristretto sẽ xóa item dựa trên độ quan trọng (TinyLFU).
		MaxCost: 1 << 30, // 1GB (1024 * 1024 * 1024)
		// BufferItems: Kích thước bộ đệm ghi. 64 là số mặc định tốt.
		BufferItems: 64,
	})
	if err != nil {
		panic(fmt.Errorf("failed to create ristretto cache: %w", err))
	}

	s := &ComputeServer{
		workerChans: make([]chan *Job, numShares),
		numShards:   numShares,
		cache:       cache,
	}

	for i := 0; i < numShares; i++ {
		s.workerChans[i] = make(chan *Job, 100) // Buffer 100 jobs per worker

		go s.startWorker(i, s.workerChans[i])
	}
	return s
}

var ChangePoolJob bool = false
var TotalMaxProcessOnWorker int = 80

func (s *ComputeServer) startWorker(id int, jobChan <-chan *Job) {

	// Nếu muốn tối ưu NUMA, có thể bind goroutine vào CPU core ở đây (nâng cao)
	for job := range jobChan {
		if ChangePoolJob {

		}

		if job == nil {
			// Defensive: never let worker crash.
			continue
		}

		ctx := job.Ctx
		if ctx == nil {
			ctx = context.Background()
		}

		if job.RespChan == nil {
			// Defensive: if producer forgot, avoid panic.
			job.RespChan = make(chan *JobResult, 1)
		}

		allowed := true
		var err error
		if err != nil {
			s.send(job, &pb.TestHTTP3Response{Status: "Error", QueryId: job.QueryId, ReceivedSize: int32(len(job.CT.Payload))}, err)
			continue
		}
		if !allowed {
			s.send(job, &pb.TestHTTP3Response{Status: "Denied", QueryId: job.QueryId, ReceivedSize: int32(len(job.CT.Payload))}, nil)
			continue
		}

		// Thực thi công việc
		// các bạn thêm các logic bussiness vào đây

		s.send(job, &pb.TestHTTP3Response{Status: "True", QueryId: job.QueryId, ReceivedSize: int32(len(job.CT.Payload))}, nil)

	}

}

func (s *ComputeServer) send(job *Job, resp *pb.TestHTTP3Response, err error) {
	if resp == nil {
		resp = &pb.TestHTTP3Response{Status: "Error", QueryId: job.QueryId}
		if err == nil {
			err = fmt.Errorf("nil response")
		}
	}
	// Always echo query_id back for tracing.
	if resp.QueryId == "" {
		resp.QueryId = job.QueryId
	}
	job.RespChan <- &JobResult{Resp: resp, Err: err}
}

func (s *ComputeServer) ExecuteQuery(ctx context.Context, req *pb.TestHTTP3Request) (*pb.TestHTTP3Response, error) {
	// 1. Sharding Algorithm: Chọn Worker dựa trên TenantID
	// Điều này đảm bảo cùng 1 Tenant luôn vào cùng 1 Worker -> Tăng Cache Hit
	shardID := int(hashTenant(req.GetQueryId()) % uint32(s.numShards))

	// NOTE: Không dùng sync.Pool cho Job vì ctx.Done() có thể khiến job bị reuse
	// trước khi worker gửi response -> race/stale response.
	job := &Job{
		Ctx:     ctx,
		QueryId: req.GetQueryId(),
		CT:      req,

		RespChan: make(chan *JobResult, 1),
	}
	if len(s.workerChans[shardID]) > TotalMaxProcessOnWorker {
		shardID = (shardID + 1) % s.numShards
		// 3. Đẩy Job vào hàng đợi của Worker tương ứng (Producer)
		select {

		case s.workerChans[shardID] <- job:
			// Đã gửi thành công
		case <-ctx.Done():
			return nil, ctx.Err() // Client hủy request
		default:
			// Backpressure: Nếu hàng đợi đầy, từ chối ngay lập tức
			return nil, fmt.Errorf("Server overloaded, please retry later")
		}

		// 4. Chờ kết quả từ Worker
		select {
		case result := <-job.RespChan:
			return result.Resp, result.Err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		select {

		case s.workerChans[shardID] <- job:
			// Đã gửi thành công
		case <-ctx.Done():
			return nil, ctx.Err() // Client hủy request
		default:
			// Backpressure: Nếu hàng đợi đầy, từ chối ngay lập tức
			return nil, fmt.Errorf("Server overloaded, please retry later")
		}

		// 4. Chờ kết quả từ Worker
		select {
		case result := <-job.RespChan:
			return result.Resp, result.Err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

}

// Hàm băm đơn giản để Sharding
func hashTenant(QueryId string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(QueryId))
	return h.Sum32()
}

func main() {
	connStr := "host=localhost port=5432 user=postgres password=Vananh12345@ dbname=laminar sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		panic(err)
	}

	fmt.Println("Connected to the database successfully!")

}

// func main() {
// 	server := NewComputeServer()

// 	const N = 1000

// 	// Đổi biến này để thấy sự khác nhau:
// 	// - true  => tất cả request chung QueryId => dồn vào 1 worker (hash sharding)
// 	// - false => mỗi request QueryId khác => phân tán đều hơn
// 	sameQueryID := false

// 	var okCount int64
// 	var errCount int64

// 	// latency stats (nano)
// 	var minNs int64 = 1<<63 - 1
// 	var maxNs int64
// 	var sumNs int64

// 	startAll := time.Now()

// 	var wg sync.WaitGroup
// 	wg.Add(N)

// 	for i := 0; i < N; i++ {
// 		i := i
// 		go func() {
// 			defer wg.Done()

// 			qid := "test-query-1"
// 			if !sameQueryID {
// 				qid = "test-query-" + strconv.Itoa(i)
// 			}

// 			// random payload 1..10MB
// 			randomNumber := rand.Intn(10) + 1
// 			req := &pb.TestHTTP3Request{
// 				QueryId: qid,
// 				Payload: make([]byte, randomNumber*1024),
// 				// nếu proto của bạn có thêm field khác thì set ở đây
// 			}

// 			t0 := time.Now()
// 			resp, err := server.ExecuteQuery(context.Background(), req)
// 			dt := time.Since(t0).Nanoseconds()

// 			atomic.AddInt64(&sumNs, dt)
// 			// update min/max (đơn giản, đủ dùng cho demo)
// 			for {
// 				cur := atomic.LoadInt64(&minNs)
// 				if dt >= cur || atomic.CompareAndSwapInt64(&minNs, cur, dt) {
// 					break
// 				}
// 			}
// 			for {
// 				cur := atomic.LoadInt64(&maxNs)
// 				if dt <= cur || atomic.CompareAndSwapInt64(&maxNs, cur, dt) {
// 					break
// 				}
// 			}

// 			if err != nil {
// 				atomic.AddInt64(&errCount, 1)
// 				return
// 			}
// 			_ = resp
// 			atomic.AddInt64(&okCount, 1)
// 		}()
// 	}

// 	wg.Wait()
// 	total := time.Since(startAll)

// 	ok := atomic.LoadInt64(&okCount)
// 	er := atomic.LoadInt64(&errCount)
// 	sum := atomic.LoadInt64(&sumNs)
// 	mn := atomic.LoadInt64(&minNs)
// 	mx := atomic.LoadInt64(&maxNs)

// 	avg := time.Duration(0)
// 	if ok+er > 0 {
// 		avg = time.Duration(sum / (ok + er))
// 	}

// 	fmt.Println("=== Load test done ===")
// 	fmt.Println("sameQueryID:", sameQueryID)
// 	fmt.Println("total:", total)
// 	fmt.Println("ok:", ok, "err:", er)
// 	fmt.Println("latency min:", time.Duration(mn))
// 	fmt.Println("latency avg:", avg)
// 	fmt.Println("latency max:", time.Duration(mx))

// 	// Giữ process sống để worker goroutine không bị kill ngay (tùy bạn)
// 	// time.Sleep(1 * time.Second)
// }
