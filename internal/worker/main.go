package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	pb "gib.comhub/shieldx-bot/laminar/pb"
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
}

func NewComputeServer() *ComputeServer {
	numShares := runtime.NumCPU()
	s := &ComputeServer{
		workerChans: make([]chan *Job, numShares),
		numShards:   numShares,
	}

	for i := 0; i < numShares; i++ {
		s.workerChans[i] = make(chan *Job, 100) // Buffer 100 jobs per worker

		go s.startWorker(i, s.workerChans[i])
	}
	return s
}

var ChangePoolJob bool = false

func (s *ComputeServer) startWorker(id int, jobChan <-chan *Job) {
	// fmt.Printf("Worker %d started on Core %d\n", id, id)

	// Nếu muốn tối ưu NUMA, có thể bind goroutine vào CPU core ở đây (nâng cao)
	for job := range jobChan {
		if ChangePoolJob {

		}

		fmt.Printf("Worker %d processing job %s | CPU Usage: %.2f%%\n", id, job.QueryId, parcents[0])
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

func main() {
	server := NewComputeServer()

	const N = 1000

	// Đổi biến này để thấy sự khác nhau:
	// - true  => tất cả request chung QueryId => dồn vào 1 worker (hash sharding)
	// - false => mỗi request QueryId khác => phân tán đều hơn
	sameQueryID := false

	var okCount int64
	var errCount int64

	// latency stats (nano)
	var minNs int64 = 1<<63 - 1
	var maxNs int64
	var sumNs int64

	startAll := time.Now()

	var wg sync.WaitGroup
	wg.Add(N)

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()

			qid := "test-query-1"
			if !sameQueryID {
				qid = "test-query-" + strconv.Itoa(i)
			}

			// random payload 1..10MB
			randomNumber := rand.Intn(10) + 1
			req := &pb.TestHTTP3Request{
				QueryId: qid,
				Payload: make([]byte, randomNumber*1024),
				// nếu proto của bạn có thêm field khác thì set ở đây
			}

			t0 := time.Now()
			resp, err := server.ExecuteQuery(context.Background(), req)
			dt := time.Since(t0).Nanoseconds()

			atomic.AddInt64(&sumNs, dt)
			// update min/max (đơn giản, đủ dùng cho demo)
			for {
				cur := atomic.LoadInt64(&minNs)
				if dt >= cur || atomic.CompareAndSwapInt64(&minNs, cur, dt) {
					break
				}
			}
			for {
				cur := atomic.LoadInt64(&maxNs)
				if dt <= cur || atomic.CompareAndSwapInt64(&maxNs, cur, dt) {
					break
				}
			}

			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			_ = resp
			atomic.AddInt64(&okCount, 1)
		}()
	}

	wg.Wait()
	total := time.Since(startAll)

	ok := atomic.LoadInt64(&okCount)
	er := atomic.LoadInt64(&errCount)
	sum := atomic.LoadInt64(&sumNs)
	mn := atomic.LoadInt64(&minNs)
	mx := atomic.LoadInt64(&maxNs)

	avg := time.Duration(0)
	if ok+er > 0 {
		avg = time.Duration(sum / (ok + er))
	}

	fmt.Println("=== Load test done ===")
	fmt.Println("sameQueryID:", sameQueryID)
	fmt.Println("total:", total)
	fmt.Println("ok:", ok, "err:", er)
	fmt.Println("latency min:", time.Duration(mn))
	fmt.Println("latency avg:", avg)
	fmt.Println("latency max:", time.Duration(mx))

	// Giữ process sống để worker goroutine không bị kill ngay (tùy bạn)
	// time.Sleep(1 * time.Second)
}
