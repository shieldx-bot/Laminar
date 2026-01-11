package main

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"math/rand"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/ristretto"
	_ "github.com/lib/pq"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github/shieldx-bot/laminar/pb"
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

func ExecuteSQLQery(query string, db *sql.DB) ([]*structpb.Struct, error) {
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

	var results []*structpb.Struct
	for _, r := range record {
		// structpb only supports JSON-like scalars; normalize ints to float64.
		rowMap := map[string]interface{}{
			"id":            float64(r.ID),
			"username":      r.USERNAME,
			"email":         r.EMAIL,
			"password_hash": r.PASSWORD_HASH,
			"balance":       float64(r.BALANCE),
			"is_active":     r.IS_ACTIVE,
			"created_at":    r.CREATED_AT,
			"updated_at":    r.UPDATED_AT,
		}

		st, err := structpb.NewStruct(rowMap)
		if err != nil {
			return nil, err
		}
		results = append(results, st)
	}

	return results, nil

}
func NewComputeServer(db *sql.DB) *ComputeServer {
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

		go s.startWorker(i, s.workerChans[i], db)
	}
	return s
}

var ChangePoolJob bool = false
var TotalMaxProcessOnWorker int = 80

func AddCacheQueryResult(cache *ristretto.Cache, query string, result *pb.TestHTTP3Response) {
	cache.Set(query, result, 1)
	cache.Wait()
}

func GetCacheQueryResult(cache *ristretto.Cache, query string) (*pb.TestHTTP3Response, bool) {
	value, found := cache.Get(query)
	if !found {
		return nil, false
	}

	resp, ok := value.(*pb.TestHTTP3Response)
	if !ok {
		return nil, false
	}

	return resp, true
}

func RemoveCacheQueryResult(cache *ristretto.Cache, query string) {
	cache.Del(query)
}

func (s *ComputeServer) startWorker(id int, jobChan <-chan *Job, db *sql.DB) {
	// 1. Kho chứa riêng (Local Queue) để worker tự sắp xếp
	var q []*Job
	useLIFO := false // Mặc định là FIFO (Công bằng)

	// Các ngưỡng để bật/tắt chế độ LIFO
	const (
		HighWaterMark = 10 // Khi hàng đợi > 10: Bật LIFO (Cứu hoả ngay lập tức)
		LowWaterMark  = 5  // Khi hàng đợi < 5: Về FIFO (Bình thường)
	)

	// Vòng lặp xử lý vô tận
	for {
		// ==========================================
		// PHA 1: HÚT VIỆC (INGESTION)
		// ==========================================

		// Nếu tay đang rỗng -> Ngủ chờ việc (Blocking)
		// Giúp tiết kiệm CPU khi không có việc
		if len(q) == 0 {
			job, ok := <-jobChan
			if !ok {
				return // Channel đóng, worker nghỉ
			}
			if job != nil {
				q = append(q, job)
			}
		}

		// Nếu đã thức, tranh thủ hút sạch việc đang chờ trong inbox (Non-blocking)
		// Mục đích: Gom việc vào để đo độ dài hàng đợi
	DrainLoop:
		for {
			select {
			case job, ok := <-jobChan:
				if !ok {
					return
				}
				if job != nil {
					q = append(q, job)
				}
			default:
				// Inbox rỗng, ngừng hút
				break DrainLoop
			}
		}

		// ==========================================
		// PHA 2: CHIẾN LƯỢC THÍCH ỨNG (ADAPTIVE SWITCHING)
		// ==========================================

		curLen := len(q)

		// Cơ chế trễ (Hysteresis) để tránh bật/tắt liên tục
		if !useLIFO && curLen >= HighWaterMark {
			useLIFO = true // BẬT LIFO: Ưu tiên người mới, bỏ mặc người cũ
			// In debug 1 lần để biết state switching
			if id == 0 { // Chỉ worker 0 in thôi cho đỡ rác
				// fmt.Printf("Worker %d switched to LIFO mode (Load: %d)\n", id, curLen)
			}
		} else if useLIFO && curLen <= LowWaterMark {
			useLIFO = false // VỀ FIFO: Quay lại công bằng
			if id == 0 {
				// fmt.Printf("Worker %d back to FIFO mode (Load: %d)\n", id, curLen)
			}
		}

		// ==========================================
		// PHA 3: CHỌN VIỆC (POP)
		// ==========================================

		var job *Job
		if useLIFO {
			// LIFO: Lấy việc ở CUỐI hàng (Mới nhất)
			lastIdx := len(q) - 1
			job = q[lastIdx]
			q = q[:lastIdx] // Cắt đuôi
		} else {
			// FIFO: Lấy việc ở ĐẦU hàng (Cũ nhất)
			job = q[0]
			q = q[1:] // Cắt đầu
		}

		// ==========================================
		// PHA 4: KIỂM TRA & CACHE (CHECK & CACHE)
		// ==========================================

		// 1. Kiểm tra khách có hủy kèo chưa (Context Done)
		select {
		case <-job.Ctx.Done():
			// Khách hủy rồi -> Bỏ qua, không làm nữa
			continue
		default:
		}

		// 2. Kiểm tra Cache Ristretto
		// Key ví dụ: "query:<query_id>"
		// cacheKey := job.CT.GetQuerySQL()
		// if val, found := s.cache.Get(cacheKey); found {
		// 	// CACHE HIT!
		// 	if resp, ok := val.(*pb.TestHTTP3Response); ok {
		// 		s.send(job, resp, nil)
		// 		continue // Xong việc này, chuyển sang việc kế tiếp ngay
		// 	}
		// }

		// ==========================================
		// PHA 5: THỰC THI (EXECUTION) - CACHE MISS
		// ==========================================

		// Giả lập xử lý nặng (Worker tốn 50ms để xử lý 1 job)
		// Đây là "Service Time" cố định.
		time.Sleep(50 * time.Millisecond)

		records, err := ExecuteSQLQery(job.CT.GetQuerySQL(), db)
		if err != nil {
			s.send(job, nil, err)
			continue
		}
		payloadSize := int32(0)
		if job.CT != nil {
			payloadSize = int32(len(job.CT.Payload))
		}

		// Tạo kết quả
		resp := &pb.TestHTTP3Response{
			Status:       "True",
			QueryId:      job.QueryId,
			ReceivedSize: payloadSize,
			Records:      records,
		}

		// 3. Lưu vào Cache với TTL (Ví dụ: 5 giây)
		// Cost = 1 (hoặc size của object). TTL = 5s.
		// s.cache.SetWithTTL(cacheKey, resp, 1, 5*time.Second)

		// Gửi trả kết quả
		s.send(job, resp, nil)
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

}

// Hàm băm đơn giản để Sharding
func hashTenant(QueryId string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(QueryId))
	return h.Sum32()
}

// Hàm tiện ích để chạy Load Test
func runLoadTest(name string, server *ComputeServer, userCount int, requestsPerUser int, sameQueryID bool) {
	fmt.Printf("\n--- START SCENARIO: %s ---\n", name)
	fmt.Printf("Users (Concurrency): %d | Reqs/User: %d | Total: %d\n", userCount, requestsPerUser, userCount*requestsPerUser)

	var (
		okCount  int64
		errCount int64
		sumNs    int64
		minNs    int64 = 1<<63 - 1
		maxNs    int64
		wg       sync.WaitGroup
		stdQuery = "SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users LIMIT 1" // Sửa query phù hợp DB của bạn
	)

	startAll := time.Now()
	wg.Add(userCount)

	for i := 0; i < userCount; i++ {
		go func(uid int) {
			defer wg.Done()

			// Tạo QueryID:
			// Trừ khi muốn test Hotspot, ta rải đều ID để tận dụng Sharding
			baseQID := "user_" + strconv.Itoa(uid)
			if sameQueryID {
				baseQID = "hot_user_vip" // Mọi người đều là user này
			}

			for j := 0; j < requestsPerUser; j++ {
				// Random nhẹ để không trùng hoàn toàn queryid trong chế độ distributed
				finalQID := baseQID
				if !sameQueryID {
					finalQID = baseQID + "_" + strconv.Itoa(j)
				}

				req := &pb.TestHTTP3Request{
					QueryId:  finalQID,
					QuerySQL: stdQuery,          // Giả sử trong proto bạn có field này hoặc hardcode
					Payload:  make([]byte, 100), // Payload nhỏ
				}

				t0 := time.Now()

				// GỌI HÀM CẦN TEST
				// CHỈNH SỬA: Đặt Timeout ngắn (200ms) để dễ gây Overload.
				// Nếu Worker (10ms) bị quá tải, job phải chờ trong hàng đợi > 190ms -> Timeout.
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				_, err := server.ExecuteQuery(ctx, req)
				cancel()

				duration := time.Since(t0).Nanoseconds()

				if err != nil {
					atomic.AddInt64(&errCount, 1)
				} else {
					atomic.AddInt64(&okCount, 1)
					atomic.AddInt64(&sumNs, duration)

					// Update Min/Max (Atomic CAS loop for accuracy)
					for {
						currMin := atomic.LoadInt64(&minNs)
						if duration >= currMin || atomic.CompareAndSwapInt64(&minNs, currMin, duration) {
							break
						}
					}
					for {
						currMax := atomic.LoadInt64(&maxNs)
						if duration <= currMax || atomic.CompareAndSwapInt64(&maxNs, currMax, duration) {
							break
						}
					}
				}
				// Sleep cực ngắn để giả lập user thật (không DDOS bản thân quá mức)
				// time.Sleep(100 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	totalTime := time.Since(startAll)

	// Tính toán thống kê
	avgNs := time.Duration(0)
	if okCount > 0 {
		avgNs = time.Duration(sumNs / okCount)
	}
	rps := float64(okCount+errCount) / totalTime.Seconds()

	fmt.Printf("Time Taken: %v\n", totalTime)
	fmt.Printf("RPS (Req/sec): %.2f\n", rps)
	fmt.Printf("Success: %d | Errors: %d\n", okCount, errCount)
	fmt.Printf("Latency: Min=%v | Avg=%v | Max=%v\n", time.Duration(minNs), avgNs, time.Duration(maxNs))
	fmt.Println("--------------------------------")
}

// Hàm chạy test hỗn hợp: 80% Hot Cache, 20% Cache Miss (Môi trường thực tế)
func runMixedLoadTest(name string, server *ComputeServer, userCount int, requestsPerUser int) {
	fmt.Printf("\n--- START SCENARIO: %s ---\n", name)
	fmt.Printf("Users: %d | Reqs/User: %d | Strategy: 80%% Hot Keys / 20%% Random Keys\n", userCount, requestsPerUser)

	var (
		okCount  int64
		errCount int64
		sumNs    int64
		minNs    int64 = 1<<63 - 1
		maxNs    int64
		wg       sync.WaitGroup
		// Query chuẩn
		stdQuery = "SELECT id, username, email, password_hash, balance, is_active, created_at, updated_at FROM users LIMIT 1"
	)

	startAll := time.Now()
	wg.Add(userCount)

	for i := 0; i < userCount; i++ {
		go func(uid int) {
			defer wg.Done()

			// Seed random riêng cho từng goroutine để tránh lock contention
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(uid)))

			for j := 0; j < requestsPerUser; j++ {
				var finalQID string

				// LOGIC 80/20:
				dice := r.Intn(100)
				if dice < 80 {
					// 80% request rơi vào 20 key HOT (Ví dụ: các sản phẩm nổi bật)
					// Giả lập chỉ có 20 Hot Key: "product_hot_0" -> "product_hot_19"
					finalQID = "product_hot_" + strconv.Itoa(r.Intn(20))
				} else {
					// 20% request là Unique/Long-tail (Ví dụ: tìm kiếm ngách)
					// Luôn tạo ra key mới -> Bắt buộc Worker phải gọi DB
					finalQID = fmt.Sprintf("long_tail_%d_%d", uid, j)
				}

				req := &pb.TestHTTP3Request{
					QueryId:  finalQID,
					QuerySQL: stdQuery,
					Payload:  make([]byte, 100),
				}

				t0 := time.Now()
				_, err := server.ExecuteQuery(context.Background(), req)
				duration := time.Since(t0).Nanoseconds()

				if err != nil {
					atomic.AddInt64(&errCount, 1)
				} else {
					atomic.AddInt64(&okCount, 1)
					atomic.AddInt64(&sumNs, duration)
					// (Giữ nguyên logic Min/Max cũ của bạn...)
					for {
						currMin := atomic.LoadInt64(&minNs)
						if duration >= currMin || atomic.CompareAndSwapInt64(&minNs, currMin, duration) {
							break
						}
					}
					for {
						currMax := atomic.LoadInt64(&maxNs)
						if duration <= currMax || atomic.CompareAndSwapInt64(&maxNs, currMax, duration) {
							break
						}
					}
				}
			}
		}(i)
	}

	wg.Wait()
	totalTime := time.Since(startAll)

	avgNs := time.Duration(0)
	if okCount > 0 {
		avgNs = time.Duration(sumNs / okCount)
	}
	rps := float64(okCount+errCount) / totalTime.Seconds()

	fmt.Printf("Time Taken: %v\n", totalTime)
	fmt.Printf("RPS (Req/sec): %.2f\n", rps)
	fmt.Printf("Success: %d | Errors: %d\n", okCount, errCount)
	fmt.Printf("Latency: Min=%v | Avg=%v | Max=%v\n", time.Duration(minNs), avgNs, time.Duration(maxNs))
	fmt.Println("--------------------------------")
}

func main() {
	// 1. SETUP DB
	connStr := "host=localhost port=5432 user=postgres password=Vananh12345@ dbname=laminar sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	// Ping để chắc chắn DB sống trước khi test
	if err := db.Ping(); err != nil {
		fmt.Println("DB Fail:", err)
	}

	rand.Seed(time.Now().UnixNano())
	server := NewComputeServer(db)
	time.Sleep(1 * time.Second)

	// =========================================================
	// KỊCH BẢN 1: LIGHT LOAD (Under Capacity)
	// Worker xử lý 20rps (50ms/req). User gửi chậm rãi.
	// =========================================================
	runLoadTest("1. LIGHT LOAD (Under Capacity)", server,
		10, // 10 Users
		20, // 10 req/user
		false,
	)

	time.Sleep(2 * time.Second)

	// =========================================================
	// KỊCH BẢN 2: SATURATION (At Capacity)
	// Tải vừa đủ để Worker làm không nghỉ, hàng đợi bắt đầu có tác dụng.
	// =========================================================
	runLoadTest("2. SATURATION POINT", server,
		20, // 20 Users (Vừa đủ vì 1s/50ms = 20 reqs/s)
		50, // 50 req/user
		false,
	)

	time.Sleep(2 * time.Second)

	// =========================================================
	// KỊCH BẢN 3: OVERLOADED & TIMEOUT (The Killing Zone)
	// Tải gấp 5 lần khả năng xử lý.
	// Worker: 20rps. Load: 100 Users ùa vào.
	// Hàng đợi đầy ngay lập tức.
	// Timeout: 200ms (chỉ đủ chờ 4 request).
	// ADAPTIVE LIFO: Sẽ chuyển sang lấy MỚI NHẤT -> Các request mới sẽ ĐẠT.
	// Request cũ (thứ 1, 2, 3...) sẽ bị bỏ lại sau và chết già trong queue.
	// =========================================================
	fmt.Println(">> Thay đổi config MaxWorker thấp xuống (Logic ảo vì Fallback đã tắt)...")
	// Giả lập overload cực mạnh
	runLoadTest("3. OVERLOAD & TIMEOUT (The Killing Zone)", server,
		100, // 100 Users đồng loạt
		20,  // Spam 20 req/user
		false,
	)

	// =========================================================
	// KỊCH BẢN 4: RECOVERY (Self-Healing)
	// (Được phản ánh qua việc Adaptive LIFO không bị chết ngạt như FIFO ở kịch bản 3)
	// =========================================================
}
