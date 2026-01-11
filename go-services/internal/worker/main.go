package worker

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"runtime"
	"time"

	"github.com/dgraph-io/ristretto"
	_ "github.com/lib/pq"
	"golang.org/x/sync/singleflight"
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
	sf          singleflight.Group
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
		HighWaterMark = 80 // Khi hàng đợi > 80: Bật LIFO (Cứu hoả)
		LowWaterMark  = 40 // Khi hàng đợi < 40: Về FIFO (Bình thường)
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
			// fmt.Printf("Worker %d switched to LIFO mode (Load: %d)\n", id, curLen)
		} else if useLIFO && curLen <= LowWaterMark {
			useLIFO = false // VỀ FIFO: Quay lại công bằng
			// fmt.Printf("Worker %d back to FIFO mode (Load: %d)\n", id, curLen)
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
		cacheKey := job.CT.GetQuerySQL()
		if val, found := s.cache.Get(cacheKey); found {
			// CACHE HIT!
			if resp, ok := val.(*pb.TestHTTP3Response); ok {
				s.send(job, resp, nil)
				continue // Xong việc này, chuyển sang việc kế tiếp ngay
			}
		}

		// ==========================================
		// PHA 5: THỰC THI (EXECUTION) - CACHE MISS
		// ==========================================

		// Giả lập xử lý nặng (DB Query, Calculation...)
		// time.Sleep(10 * time.Millisecond) // Uncomment để test delay
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
		s.cache.SetWithTTL(cacheKey, resp, 1, 5*time.Second)

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
	// Kỹ thuật Request Coalescing (SingleFlight)
	// Chống Thundering Herd: Gộp các request cùng QuerySQL thành 1 execution
	key := req.GetQuerySQL()
	if key == "" {
		key = req.GetQueryId() // Fallback
	}

	result, err, _ := s.sf.Do(key, func() (interface{}, error) {
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
	})

	if err != nil {
		return nil, err
	}

	// Trả về kết quả (đảm bảo QueryId đúng với request gốc của user)
	originResp := result.(*pb.TestHTTP3Response)
	return &pb.TestHTTP3Response{
		Status:       originResp.Status,
		QueryId:      req.GetQueryId(), // Trả lại ID riêng của từng request
		Records:      originResp.Records,
		ReceivedSize: originResp.ReceivedSize,
	}, nil
}

// Hàm băm đơn giản để Sharding
func hashTenant(QueryId string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(QueryId))
	return h.Sum32()
}
