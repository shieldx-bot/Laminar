package main

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	pb "github/shieldx-bot/laminar/pb"
)

// UnboundedServer simulates a server that handles every request in a new goroutine
// immediately, without any queueing or pooling limit.
type UnboundedServer struct {
}

func NewUnboundedServer() *UnboundedServer {
	return &UnboundedServer{}
}

func (s *UnboundedServer) ExecuteQuery(ctx context.Context, req *pb.TestHTTP3Request) (*pb.TestHTTP3Response, error) {
	// Unbounded Model:
	// We simulate the behavior where the server eagerly accepts the request and
	// allocates resources for it immediately.

	// Simulation:
	// 1. Heavy Memory Allocation (to stress GC)
	// Allocate 10KB per request. 50k requests = 500MB roughly + Metadata.
	// This simulates "per-request context" overhead (buffers, parsing, logic).
	// Force Escape: 200 KB
	payload := make([]byte, 200*1024)
	for i := range payload {
		payload[i] = byte(i)
	}

	// 2. Simulate Latency (DB IO / CPU)
	// Represents processing time where the memory is held.
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	_ = payload[0]

	return &pb.TestHTTP3Response{
		QueryId: req.QueryId,
		Status:  "OK",
		// Payload removed
	}, nil
}

func main() {
	// Adjust GOMAXPROCS if needed, though default is NumCPU
	fmt.Printf("CPUs: %d\n", runtime.NumCPU())

	server := NewUnboundedServer()

	// Simulation Parameters
	const ConcurrentRequests = 50000

	var wg sync.WaitGroup
	wg.Add(ConcurrentRequests)

	// Metrics
	var successCount int32
	var errorCount int32

	// 1. Initial Memory Snapshot
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	fmt.Printf("Start: Alloc = %v MiB, TotalAlloc = %v MiB\n", bToMb(m1.Alloc), bToMb(m1.TotalAlloc))

	startTime := time.Now()

	// Monitor Memory Peak in background
	doneMonitor := make(chan bool)
	var peakAlloc uint64
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-doneMonitor:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				// Atomic or simple check strictly for logging
				if m.Alloc > peakAlloc {
					peakAlloc = m.Alloc
				}
				// Also check Sys or other stats if needed
			}
		}
	}()

	// Launch 50k requests "simulating" unbounded acceptance
	// In a real server, the net/http or gRPC listener would spawn these.
	fmt.Printf("Launching %d requests...\n", ConcurrentRequests)
	for i := 0; i < ConcurrentRequests; i++ {
		go func(id int) {
			defer wg.Done()
			req := &pb.TestHTTP3Request{QueryId: fmt.Sprintf("req-%d", id)}
			// Unbounded timeouts often happen if machine chokes, but we set a generous context
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			_, err := server.ExecuteQuery(ctx, req)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			} else {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()
	close(doneMonitor)
	duration := time.Since(startTime)

	// Final Stats
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	fmt.Println("----- Unbounded Concurrency Results -----")
	fmt.Printf("Requests: %d\n", ConcurrentRequests)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Errors: %d\n", errorCount)
	fmt.Printf("Peak Memory Alloc: %v MiB\n", bToMb(peakAlloc))
	fmt.Printf("Final Memory Alloc: %v MiB\n", bToMb(m2.Alloc))
	fmt.Printf("Peak Stack Inuse: %v MiB\n", bToMb(m2.StackInuse)) // Added Stack check
	fmt.Printf("GC Pause Total: %v ms\n", float64(m2.PauseTotalNs-m1.PauseTotalNs)/1e6)
	fmt.Printf("NumGC: %v\n", m2.NumGC-m1.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
