package worker

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dgraph-io/ristretto"
	_ "github.com/lib/pq"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	pb "github/shieldx-bot/laminar/pb"
)

type Job struct {
	Ctx      context.Context
	QueryId  string
	Action   string
	CT       *pb.CallBackRequest
	RespChan chan *JobResult
}

type JobResult struct {
	Resp *pb.CallBackResponse
	Err  error
}

type ComputeServer struct {
	pb.UnimplementedLaminarGatewayServer
	workerChans []chan *Job
	numShards   int
	cache       *ristretto.Cache
	cacheTTL    time.Duration

	// IAT_exec: inter-arrival time at execution start (global across all workers).
	// Measured at the exact point a job begins actual execution (after ctx check,
	// before cache/DB work). Spikes indicate time-fragmentation/jitter.
	lastExecStartNs uint64
	iatCount        uint64
	iatSumNs        uint64
	iatMaxNs        uint64
	iatB1ms         uint64 // <= 1ms
	iatB5ms         uint64 // <= 5ms
	iatB10ms        uint64 // <= 10ms
	iatB25ms        uint64 // <= 25ms
	iatB50ms        uint64 // <= 50ms
	iatBgt50ms      uint64 // > 50ms

	// Queue depth: number of requests that have reached backend but have NOT
	// been allocated resources to run yet.
	// Definition here: successful enqueue increments; POP (dequeue) decrements.
	queueDepth int64

	// Event-based sampling of queue depth at enqueue/dequeue for percentiles.
	qdEventCount uint64
	qdSum        uint64
	qdMax        uint64
	qdBuckets    [11]uint64
}

var qdBucketUpperBounds = [11]int64{0, 1, 2, 3, 4, 9, 19, 39, 79, 159, 1 << 62}

func qdBucketIndex(qd int64) int {
	switch {
	case qd <= 0:
		return 0
	case qd <= 1:
		return 1
	case qd <= 2:
		return 2
	case qd <= 3:
		return 3
	case qd <= 4:
		return 4
	case qd <= 9:
		return 5
	case qd <= 19:
		return 6
	case qd <= 39:
		return 7
	case qd <= 79:
		return 8
	case qd <= 159:
		return 9
	default:
		return 10
	}
}

func (s *ComputeServer) observeQueueDepth(qd int64) {
	if qd < 0 {
		qd = 0
	}
	atomic.AddUint64(&s.qdEventCount, 1)
	atomic.AddUint64(&s.qdSum, uint64(qd))
	atomic.AddUint64(&s.qdBuckets[qdBucketIndex(qd)], 1)

	qdU := uint64(qd)
	for {
		old := atomic.LoadUint64(&s.qdMax)
		if qdU <= old {
			break
		}
		if atomic.CompareAndSwapUint64(&s.qdMax, old, qdU) {
			break
		}
	}
}

func (s *ComputeServer) onEnqueue() {
	qd := atomic.AddInt64(&s.queueDepth, 1)
	s.observeQueueDepth(qd)
}

func (s *ComputeServer) onDequeue() {
	qd := atomic.AddInt64(&s.queueDepth, -1)
	if qd < 0 {
		atomic.StoreInt64(&s.queueDepth, 0)
		qd = 0
	}
	s.observeQueueDepth(qd)
}

func (s *ComputeServer) recordExecStart() {
	nowNs := uint64(time.Now().UnixNano())
	prevNs := atomic.SwapUint64(&s.lastExecStartNs, nowNs)
	if prevNs == 0 {
		return
	}
	if nowNs <= prevNs {
		return
	}

	d := time.Duration(int64(nowNs - prevNs))
	atomic.AddUint64(&s.iatCount, 1)
	atomic.AddUint64(&s.iatSumNs, uint64(d.Nanoseconds()))

	// max (lock-free)
	maxNs := uint64(d.Nanoseconds())
	for {
		old := atomic.LoadUint64(&s.iatMaxNs)
		if maxNs <= old {
			break
		}
		if atomic.CompareAndSwapUint64(&s.iatMaxNs, old, maxNs) {
			break
		}
	}

	ms := d.Milliseconds()
	switch {
	case ms <= 1:
		atomic.AddUint64(&s.iatB1ms, 1)
	case ms <= 5:
		atomic.AddUint64(&s.iatB5ms, 1)
	case ms <= 10:
		atomic.AddUint64(&s.iatB10ms, 1)
	case ms <= 25:
		atomic.AddUint64(&s.iatB25ms, 1)
	case ms <= 50:
		atomic.AddUint64(&s.iatB50ms, 1)
	default:
		atomic.AddUint64(&s.iatBgt50ms, 1)
	}
}

func (s *ComputeServer) startIATExecLogger() {
	// Logs windowed stats every 5 seconds.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		count := atomic.SwapUint64(&s.iatCount, 0)
		sumNs := atomic.SwapUint64(&s.iatSumNs, 0)
		maxNs := atomic.SwapUint64(&s.iatMaxNs, 0)
		b1 := atomic.SwapUint64(&s.iatB1ms, 0)
		b5 := atomic.SwapUint64(&s.iatB5ms, 0)
		b10 := atomic.SwapUint64(&s.iatB10ms, 0)
		b25 := atomic.SwapUint64(&s.iatB25ms, 0)
		b50 := atomic.SwapUint64(&s.iatB50ms, 0)
		bgt := atomic.SwapUint64(&s.iatBgt50ms, 0)

		var avgMs float64
		if count > 0 {
			avgMs = float64(sumNs) / float64(count) / 1e6
		}

		log.Printf(
			"[iat_exec] window=5s starts=%d avg_ms=%.3f max_ms=%.3f buckets<=1=%d <=5=%d <=10=%d <=25=%d <=50=%d >50=%d",
			count,
			avgMs,
			float64(maxNs)/1e6,
			b1, b5, b10, b25, b50, bgt,
		)
	}
}

func (s *ComputeServer) startQueueDepthLogger() {
	// Logs event-based queue depth percentiles every 5 seconds.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		count := atomic.SwapUint64(&s.qdEventCount, 0)
		sum := atomic.SwapUint64(&s.qdSum, 0)
		max := atomic.SwapUint64(&s.qdMax, 0)
		var buckets [11]uint64
		for i := 0; i < len(buckets); i++ {
			buckets[i] = atomic.SwapUint64(&s.qdBuckets[i], 0)
		}

		var avg float64
		if count > 0 {
			avg = float64(sum) / float64(count)
		}

		p95 := int64(0)
		p99 := int64(0)
		if count > 0 {
			p95Target := uint64(float64(count) * 0.95)
			p99Target := uint64(float64(count) * 0.99)
			if p95Target == 0 {
				p95Target = 1
			}
			if p99Target == 0 {
				p99Target = 1
			}

			var cum uint64
			for i := 0; i < len(buckets); i++ {
				cum += buckets[i]
				if p95 == 0 && cum >= p95Target {
					p95 = qdBucketUpperBounds[i]
				}
				if p99 == 0 && cum >= p99Target {
					p99 = qdBucketUpperBounds[i]
					break
				}
			}
		}

		current := atomic.LoadInt64(&s.queueDepth)
		log.Printf(
			"[queue_depth] window=5s events=%d current=%d avg=%.2f p95<=%d p99<=%d max=%d",
			count,
			current,
			avg,
			p95,
			p99,
			max,
		)
	}
}

var cpuPctBucketUpperBounds = [...]float64{0, 1, 2, 5, 10, 20, 40, 60, 80, 100, 150, 200, 400, 1e9}

func cpuPctBucketIndex(p float64) int {
	if p <= 0 {
		return 0
	}
	for i := 1; i < len(cpuPctBucketUpperBounds); i++ {
		if p <= cpuPctBucketUpperBounds[i] {
			return i
		}
	}
	return len(cpuPctBucketUpperBounds) - 1
}

func cpuTimeMicros(ru *syscall.Rusage) int64 {
	ut := int64(ru.Utime.Sec)*1_000_000 + int64(ru.Utime.Usec)
	st := int64(ru.Stime.Sec)*1_000_000 + int64(ru.Stime.Usec)
	return ut + st
}

func processRSSBytes() (uint64, bool) {
	// Linux best-effort: /proc/self/statm => resident pages
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}
	rssPages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return rssPages * uint64(os.Getpagesize()), true
}

func (s *ComputeServer) startCPUP95Logger() {
	// CPU P95 + CPU avg sampled from process CPU time (rusage) deltas.
	// Percent can exceed 100 on multi-core.
	// RAM avg is RSS avg (best-effort via /proc); falls back to Go runtime mem stats.
	const sampleEvery = 200 * time.Millisecond
	const windowEvery = 5 * time.Second

	sampleTicker := time.NewTicker(sampleEvery)
	windowTicker := time.NewTicker(windowEvery)
	defer sampleTicker.Stop()
	defer windowTicker.Stop()

	var prevRU syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &prevRU); err != nil {
		log.Printf("[cpu_p95] disabled: getrusage error: %v", err)
		return
	}
	prevCPUus := cpuTimeMicros(&prevRU)
	prevWall := time.Now()

	var buckets [len(cpuPctBucketUpperBounds)]uint64
	var sampleCount uint64
	var maxPct float64
	var sumPct float64

	var ramSumBytes uint64
	var ramSamples uint64
	var ramMaxBytes uint64

	for {
		select {
		case <-sampleTicker.C:
			var ru syscall.Rusage
			if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
				continue
			}
			now := time.Now()
			cpuUs := cpuTimeMicros(&ru)
			dCPU := cpuUs - prevCPUus
			dWallUs := now.Sub(prevWall).Microseconds()
			prevCPUus = cpuUs
			prevWall = now

			if dCPU < 0 || dWallUs <= 0 {
				continue
			}
			pct := (float64(dCPU) / float64(dWallUs)) * 100.0
			if pct < 0 {
				pct = 0
			}
			if pct > maxPct {
				maxPct = pct
			}
			sumPct += pct
			buckets[cpuPctBucketIndex(pct)]++
			sampleCount++

			// RAM sampling
			var rss uint64
			if v, ok := processRSSBytes(); ok {
				rss = v
			} else {
				var ms runtime.MemStats
				runtime.ReadMemStats(&ms)
				rss = ms.Sys
			}
			ramSumBytes += rss
			ramSamples++
			if rss > ramMaxBytes {
				ramMaxBytes = rss
			}

		case <-windowTicker.C:
			if sampleCount == 0 {
				log.Printf("[cpu] window=5s samples=0")
				log.Printf("[ram] window=5s samples=0")
				continue
			}
			target := uint64(float64(sampleCount) * 0.95)
			if target == 0 {
				target = 1
			}
			var cum uint64
			p95Upper := cpuPctBucketUpperBounds[len(cpuPctBucketUpperBounds)-1]
			for i := 0; i < len(buckets); i++ {
				cum += buckets[i]
				if cum >= target {
					p95Upper = cpuPctBucketUpperBounds[i]
					break
				}
			}
			cpuAvg := sumPct / float64(sampleCount)
			log.Printf("[cpu] window=5s samples=%d avg=%.1f p95<=%.0f max=%.1f", sampleCount, cpuAvg, p95Upper, maxPct)

			if ramSamples > 0 {
				ramAvgMb := (float64(ramSumBytes) / float64(ramSamples)) / 1024.0 / 1024.0
				ramMaxMb := float64(ramMaxBytes) / 1024.0 / 1024.0
				log.Printf("[ram] window=5s samples=%d avg_mb=%.1f max_mb=%.1f", ramSamples, ramAvgMb, ramMaxMb)
			} else {
				log.Printf("[ram] window=5s samples=0")
			}

			// reset window
			for i := range buckets {
				buckets[i] = 0
			}
			sampleCount = 0
			maxPct = 0
			sumPct = 0
			ramSumBytes = 0
			ramSamples = 0
			ramMaxBytes = 0
		}
	}
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
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	})
	if err != nil {
		cache = nil
	}

	s := &ComputeServer{
		workerChans: make([]chan *Job, numShares),
		numShards:   numShares,
		cache:       cache,
		cacheTTL:    20 * time.Second,
	}

	go s.startIATExecLogger()
	go s.startQueueDepthLogger()
	go s.startCPUP95Logger()

	for i := 0; i < numShares; i++ {
		s.workerChans[i] = make(chan *Job, 100) // Buffer 100 jobs per worker

		go s.startWorker(i, s.workerChans[i], db)
	}
	return s
}

var ChangePoolJob bool = false
var TotalMaxProcessOnWorker int = 80

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

		// Job leaves the queue here (resource allocation can begin).
		s.onDequeue()

		// ==========================================
		// PHA 4: KIỂM TRA (CHECK)
		// ==========================================

		// 1. Kiểm tra khách có hủy kèo chưa (Context Done)
		select {
		case <-job.Ctx.Done():
			// Khách hủy rồi -> Bỏ qua, không làm nữa
			continue
		default:
		}

		// ==========================================
		// PHA 5: THỰC THI (EXECUTION)
		// ==========================================
		s.recordExecStart()

		queryKey := ""
		if job.CT != nil {
			queryKey = job.CT.GetQuerySQL()
		}

		if s.cache != nil && s.cacheTTL > 0 && queryKey != "" {
			if val, ok := s.cache.Get(queryKey); ok {
				if cachedBytes, ok := val.([]byte); ok {
					var cachedResp pb.CallBackResponse
					if err := proto.Unmarshal(cachedBytes, &cachedResp); err == nil {
						cachedResp.QueryId = job.QueryId
						// Fix: Thêm Urlcallback vào response trả về từ Cache
						cachedResp.Urlcallback = job.CT.GetUrlcallback()
						s.send(job, &cachedResp, nil)
						continue
					}
				}
			}
		}

		// Giả lập xử lý nặng (DB Query, Calculation...)
		// time.Sleep(10 * time.Millisecond) // Uncomment để test delay
		records, err := ExecuteSQLQery(job.CT.GetQuerySQL(), db)
		if err != nil {
			s.send(job, nil, err)
			continue
		}

		// Tạo kết quả
		resp := &pb.CallBackResponse{
			QueryId:     job.QueryId,
			Records:     records,
			Urlcallback: job.CT.GetUrlcallback(),
		}

		if s.cache != nil && s.cacheTTL > 0 && queryKey != "" {
			cacheResp := &pb.CallBackResponse{
				Records: resp.Records,
			}
			if buf, err := proto.Marshal(cacheResp); err == nil {
				s.cache.SetWithTTL(queryKey, buf, int64(len(buf)), s.cacheTTL)
			}
		}

		// Gửi trả kết quả
		s.send(job, resp, nil)
	}

}
func (s *ComputeServer) send(job *Job, resp *pb.CallBackResponse, err error) {
	if resp == nil {
		resp = &pb.CallBackResponse{QueryId: job.QueryId}
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

func (s *ComputeServer) ExecuteQuery(ctx context.Context, req *pb.CallBackRequest) (*pb.CallBackResponse, error) {
	// 1. Sharding Algorithm: Chọn Worker dựa trên QueryId
	// Điều này đảm bảo cùng 1 QueryId luôn vào cùng 1 Worker -> Tăng Cache Hit
	shardKey := req.GetQuerySQL()
	if shardKey == "" {
		shardKey = req.GetQueryId()
	}
	shardID := int(hashTenant(shardKey) % uint32(s.numShards))

	job := &Job{
		Ctx:      ctx,
		QueryId:  req.GetQueryId(),
		CT:       req,
		RespChan: make(chan *JobResult, 1),
	}

	if len(s.workerChans[shardID]) > TotalMaxProcessOnWorker {
		shardID = (shardID + 1) % s.numShards
	}

	// 2. Đẩy Job vào hàng đợi của Worker tương ứng (Producer)
	select {
	case s.workerChans[shardID] <- job:
		// Đã gửi thành công
		s.onEnqueue()
	case <-ctx.Done():
		return nil, ctx.Err() // Client hủy request
	default:
		// Backpressure: Nếu hàng đợi đầy, từ chối ngay lập tức
		return nil, fmt.Errorf("Server overloaded, please retry later")
	}

	// 3. Chờ kết quả từ Worker
	select {
	case result := <-job.RespChan:
		if result.Err != nil {
			return nil, result.Err
		}
		originResp := result.Resp
		return &pb.CallBackResponse{
			QueryId:     req.GetQueryId(),
			Records:     originResp.Records,
			Urlcallback: originResp.GetUrlcallback(),
		}, nil
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
