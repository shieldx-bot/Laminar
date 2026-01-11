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

// Job represents the unit of work
type Job struct {
	Ctx      context.Context
	QueryId  string
	RespChan chan error
}

// ComputeServer with Worker Pool
type ComputeServer struct {
	jobChan chan *Job
}

func NewComputeServer() *ComputeServer {
	// Worker Pool Size = NumCPU (e.g., 8-16)
	// This dramatically limits concurrent "Heavy Allocations" compared to 50k.
	numWorkers := runtime.NumCPU()
	s := &ComputeServer{
		jobChan: make(chan *Job), // Unbuffered or Small Buffer (e.g. 100)
	}

	// Start Workers
	for i := 0; i < numWorkers; i++ {
		go s.worker(i)
	}
	return s
}

func (s *ComputeServer) worker(id int) {
	for job := range s.jobChan {
		// Check if job expired while waiting in queue/handover
		select {
		case <-job.Ctx.Done():
			job.RespChan <- job.Ctx.Err()
			continue
		default:
		}

		// --- WORKLOAD (Identical to Unbounded) ---
		// Allocate 200KB per request.
		payload := make([]byte, 200*1024)
		for i := range payload {
			payload[i] = byte(i)
		}

		// Simulate Latency
		select {
		case <-time.After(500 * time.Millisecond):
		case <-job.Ctx.Done():
			job.RespChan <- job.Ctx.Err()
			continue
		}

		// Prevent compiler optimization
		// Return in response (but Response is constructed in ExecuteQuery?)
		// Ah, worker does NOT construct response currently.
		// worker loop sends `job.RespChan <- nil`.
		// The `ExecuteQuery` constructs the response.
		// THIS IS A DIFFERENCE.

		// To match Unbounded logic where the WORKER holds the memory:
		// We need to ensure payload is live.
		// Simple way: assign to a field in job?
		// Or assume _ = payload[0] + 500ms sleep is enough.
		// But in Unbounded, I assigned it to `resp.Payload`.

		// Let's change worker signature to return payload?
		// No, `job.RespChan` is `chan error`.
		// I need to change `Job` struct to carry result?
		// Or just stick to `_ = payload[0]` but make sure it escapes?

		// If I use `fmt.Sprintf("%v", payload[0])` it's weak.
		// How about `globalVar = payload`? (Race condition, but forces heap).

		// Better: Change Job to have `Result interface{}`
		// But I want to minimize refactoring.
		// In Unbounded, `ExecuteQuery` returns the payload.
		// In Worker Pool, `ExecuteQuery` creates `TestHTTP3Response`.
		// The WORKER is where the heavy lifting happens.
		// Theoretically both should behave same for MEMORY if I just hold it during sleep.

		// I'll stick to `_ = payload[0]` but verify escape analysis?
		// Actually, let's just use `blackhole(payload)` function to be sure.

		// But wait, in Unbounded I changed it to `Payload: payload`.
		// If I don't do that here, it's unfair comparison (maybe).
		// But `TestHTTP3Response` is created in `ExecuteQuery` (Producer).
		// The worker doesn't touch the response.
		// If I want to simulate "Processing requires memory", the worker allocates it.
		// If the worker discards it, but holds it during sleep, that's valid "Working Memory".

		// So `_ = payload[0]` IS valid for "Working Set".
		// The fact Unbounded returns it keeps it alive LONGER (until ExecuteQuery returns).
		// Code:
		// Unbounded: Alloc -> Sleep -> Return (Live until return).
		// Worker: Alloc -> Sleep -> Discard -> Signal Done.
		// The "Live Duration" is the same (Sleep duration).

		// I will just use `_ = payload[0]` here.
		// And rely on 500ms sleep to overlap them.

		_ = payload[0]
		// ------------------------------------

		job.RespChan <- nil
	}
}

func (s *ComputeServer) ExecuteQuery(ctx context.Context, req *pb.TestHTTP3Request) (*pb.TestHTTP3Response, error) {
	// Producer
	job := &Job{
		Ctx:      ctx,
		QueryId:  req.QueryId,
		RespChan: make(chan error, 1),
	}

	// Submit to pool
	// The 50,000 caller goroutines will BLOCK here if workers are busy.
	// They consume 2KB stack each, but NOT the 10KB payload.
	select {
	case s.jobChan <- job:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Wait for result
	select {
	case err := <-job.RespChan:
		if err != nil {
			return nil, err
		}
		return &pb.TestHTTP3Response{QueryId: req.QueryId, Status: "OK"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func main() {
	// Adjust GOMAXPROCS if needed
	fmt.Printf("CPUs: %d\n", runtime.NumCPU())

	server := NewComputeServer()

	// Simulation Parameters (MUST MATCH UNBOUNDED)
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
				if m.Alloc > peakAlloc {
					peakAlloc = m.Alloc
				}
			}
		}
	}()

	// Launch 50k requests
	fmt.Printf("Launching %d requests...\n", ConcurrentRequests)
	for i := 0; i < ConcurrentRequests; i++ {
		go func(id int) {
			defer wg.Done()
			req := &pb.TestHTTP3Request{QueryId: fmt.Sprintf("req-%d", id)}
			// Give it enough time to process all
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

	fmt.Println("----- Worker Pool Results -----")
	fmt.Printf("Requests: %d\n", ConcurrentRequests)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Errors: %d\n", errorCount)
	fmt.Printf("Peak Memory Alloc: %v MiB\n", bToMb(peakAlloc))
	fmt.Printf("Peak Stack Inuse: %v MiB\n", bToMb(m2.StackInuse))
	fmt.Printf("GC Pause Total: %v ms\n", float64(m2.PauseTotalNs-m1.PauseTotalNs)/1e6)
	fmt.Printf("NumGC: %v\n", m2.NumGC-m1.NumGC)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}
