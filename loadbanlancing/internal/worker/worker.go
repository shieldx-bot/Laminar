package worker

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	cgvnode "github/shieldx-bot/loadbanlacing/internal/cg-vnode"
	pb "github/shieldx-bot/loadbanlacing/pb"

	"google.golang.org/protobuf/encoding/protojson"
)

type VPS struct {
	IP     string
	caches []ListCachesOnServer
}

type ListCachesOnServer struct {
	Timestamp time.Time
	queryKey  string
}

var ListVPS = []VPS{
	{IP: "backend1", caches: []ListCachesOnServer{}},
	{IP: "backend2", caches: []ListCachesOnServer{}},
	{IP: "backend3", caches: []ListCachesOnServer{}},
	{IP: "backend4", caches: []ListCachesOnServer{}},
	{IP: "backend5", caches: []ListCachesOnServer{}},
	{IP: "backend6", caches: []ListCachesOnServer{}},
	{IP: "backend7", caches: []ListCachesOnServer{}},
	{IP: "backend8", caches: []ListCachesOnServer{}},
	{IP: "backend9", caches: []ListCachesOnServer{}},
	{IP: "backend10", caches: []ListCachesOnServer{}},
	{IP: "backend11", caches: []ListCachesOnServer{}},
	{IP: "backend12", caches: []ListCachesOnServer{}},
	{IP: "backend13", caches: []ListCachesOnServer{}},
	{IP: "backend14", caches: []ListCachesOnServer{}},
	{IP: "backend15", caches: []ListCachesOnServer{}},
	{IP: "backend16", caches: []ListCachesOnServer{}},
	{IP: "backend17", caches: []ListCachesOnServer{}},
	{IP: "backend18", caches: []ListCachesOnServer{}},
	{IP: "backend19", caches: []ListCachesOnServer{}},
	{IP: "backend20", caches: []ListCachesOnServer{}},
}

var RamdomVPS bool = true

var listVPSMu sync.RWMutex

func hasVPS(ip string) bool {
	listVPSMu.RLock()
	defer listVPSMu.RUnlock()
	for _, v := range ListVPS {
		if v.IP == ip {
			return true
		}
	}
	return false
}

func addCacheToVPS(ip string, queryKey string) {
	listVPSMu.Lock()
	defer listVPSMu.Unlock()
	for i, v := range ListVPS {
		if v.IP == ip {
			ListVPS[i].caches = append(ListVPS[i].caches, ListCachesOnServer{
				Timestamp: time.Now(),
				queryKey:  queryKey,
			})
			break
		}
	}
}
func removeCacheInVPS(ip string, queryKey string) {
	listVPSMu.Lock()
	defer listVPSMu.Unlock()
	for i, v := range ListVPS {
		if v.IP == ip {
			for j, c := range v.caches {
				if c.queryKey == queryKey {
					// Xoá cache khỏi slice
					ListVPS[i].caches = append(ListVPS[i].caches[:j], ListVPS[i].caches[j+1:]...)
					return
				}
			}
		}
	}
}
func HasCacheInVPS(queryKey string) []string {
	listVPSMu.RLock()
	var result []string
	for _, v := range ListVPS {
		for _, c := range v.caches {
			if time.Since(c.Timestamp) > 5*time.Minute {
				removeCacheInVPS(v.IP, queryKey)
			} else if c.queryKey == queryKey {
				result = append(result, c.queryKey)
			}
		}

	}

	return result
}

type Job struct {
	Ctx      context.Context
	QueryId  string
	QuerySQL string
	CT       *pb.RequestToBalancer
	RespChan chan *JobResult
}

type JobResult struct {
	Resp *pb.ResponseToBalancer
	err  error
}

type ComputeServer struct {
	pb.UnimplementedLaminarGatewayServer
	workerChans []chan *Job
	numShards   int
}

// ==== ===============  JOB TO BACKEND  ================
type JobToBackend struct {
	IPbackend []string
	Req       *pb.RequestToBalancer
}

var JobBackendChan chan *JobToBackend = make(chan *JobToBackend, 1000)

func StartJobToBackendWorker() {
	for {
		job, ok := <-JobBackendChan
		if !ok {
			return
		}
		if job != nil {
			// Xử lý job gửi đến backend
			for _, ip := range job.IPbackend {
				url := ip
				if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
					url = "http://" + url
				}

				body, err := protojson.Marshal(job.Req)
				if err != nil {
					continue
				}

				req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
				if err != nil {
					continue
				}
				req.Header.Set("Content-Type", "application/json")

			}

		}
	}
}

func init() {
	go StartJobToBackendWorker()
}

// ==== =============== HASHING VNODE & GET NODE ================

func GetNode(key string) []string {
	listVPSMu.RLock()
	defer listVPSMu.RUnlock()
	listIPs := make([]string, 0, len(ListVPS))
	for _, v := range ListVPS {
		listIPs = append(listIPs, v.IP)
	}
	ring := cgvnode.NewHashRing(listIPs, 100)
	selected := ring.GetNode(key)
	result := make([]string, 0, len(selected))
	for _, n := range selected {
		result = append(result, n.Key)
	}
	result = append(result, HasCacheInVPS(key)...)

	return result
}

func NewComputeServer() *ComputeServer {
	numShares := runtime.NumCPU()

	s := &ComputeServer{
		workerChans: make([]chan *Job, numShares),
		numShards:   numShares,
	}
	for i := 0; i < numShares; i++ {
		s.workerChans[i] = make(chan *Job, 200) // buffer size 200
		go s.startWorker(i, s.workerChans[i])
	}
	return s
}

func (s *ComputeServer) startWorker(shardID int, jobChan <-chan *Job) {
	var q []*Job
	useLIFO := false // Mặc định là FIFO (Công bằng)

	const (
		HighWaterMark = 80 // Khi hàng đợi > 80: Bật LIFO (Cứu hoả)
		LowWaterMark  = 40 // Khi hàng đợi < 40: Về FIFO (Bình thường)
	)
	for {
		job, ok := <-jobChan
		if !ok {
			return
		}
		if job != nil {
			q = append(q, job)
		}

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
				break DrainLoop
			}
		}

		curLen := len(q)
		if !useLIFO && curLen > HighWaterMark {
			useLIFO = true
		} else if useLIFO && curLen < LowWaterMark {
			useLIFO = false
		}

		var nextJob *Job

		if useLIFO {
			lastIndex := len(q) - 1
			nextJob = q[lastIndex]
			q = q[:lastIndex] // Cắt đuôi
		} else {
			nextJob = q[0]
			q = q[1:] // Cắt đầu
		}

		//  Kiểm tra check
		select {
		case <-nextJob.Ctx.Done():
			continue
		default:

		}

		// Xử lý công việc

		Node := GetNode(nextJob.QuerySQL)
		if len(Node) == 0 {
			err := fmt.Errorf("No available backend nodes")
			s.send(nextJob, nil, err)
			continue
		}

		time.Sleep(2000 * time.Millisecond) // Giả lập delay xử lý
		NumberRamdom := rand.Intn(100)

		status := "202 Accepted: " + fmt.Sprint(NumberRamdom)

		req := &pb.ResponseToBalancer{Status: status}

		s.send(nextJob, req, nil)
	}
}

func (s *ComputeServer) send(job *Job, resp *pb.ResponseToBalancer, err error) {
	job.RespChan <- &JobResult{
		Resp: resp,
		err:  err,
	}

}

func (s *ComputeServer) SubmitJob(ctx context.Context, req *pb.RequestToBalancer) (*pb.ResponseToBalancer, error) {
	shardKey := req.QuerySQL
	if shardKey == "" {
		shardKey = req.QueryId
	}

	shardIdx := int(hashTenant(shardKey)) % s.numShards

	job := &Job{
		Ctx:      ctx,
		QueryId:  req.QueryId,
		QuerySQL: req.QuerySQL,
		CT:       req,
		RespChan: make(chan *JobResult, 1),
	}
	select {
	case s.workerChans[shardIdx] <- job:
		// Đã gửi thành công
	case <-ctx.Done():
		// Client hủy request

	default:
		return nil, fmt.Errorf("Server overloaded, please retry later")
	}

	// Chờ kết quả từ Worker
	select {
	case result := <-job.RespChan:
		if result.err != nil {
			return nil, result.err
		}
		return result.Resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()

	}

}

func hashTenant(QueryId string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(QueryId))
	return h.Sum32()
}
