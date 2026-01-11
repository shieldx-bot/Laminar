package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github/shieldx-bot/gateway/pb"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq" // Driver postgres
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var testHTTP3SingleFlight singleflight.Group
var queryCache *ristretto.Cache

var (
	inflightHTTPRequests int64
	cacheHits            int64
	cacheMisses          int64
	sfSharedCount        int64
	sfLeaderCount        int64
	grpcOKCount          int64
	grpcErrCount         int64
	grpcDeadlineCount    int64
	grpcCanceledCount    int64
)

type tcpConnStats struct {
	Total       int            `json:"total"`
	ByStateName map[string]int `json:"by_state"`
}

func tcpStateName(hex string) string {
	switch strings.ToUpper(hex) {
	case "01":
		return "ESTABLISHED"
	case "02":
		return "SYN_SENT"
	case "03":
		return "SYN_RECV"
	case "04":
		return "FIN_WAIT1"
	case "05":
		return "FIN_WAIT2"
	case "06":
		return "TIME_WAIT"
	case "07":
		return "CLOSE"
	case "08":
		return "CLOSE_WAIT"
	case "09":
		return "LAST_ACK"
	case "0A":
		return "LISTEN"
	case "0B":
		return "CLOSING"
	default:
		return "UNKNOWN"
	}
}

func countTCPConnsForLocalPort(port uint16) tcpConnStats {
	stats := tcpConnStats{ByStateName: map[string]int{}}
	paths := []string{"/proc/net/tcp", "/proc/net/tcp6"}
	portHex := strings.ToUpper(fmt.Sprintf("%04X", port))

	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for i, line := range lines {
			if i == 0 || strings.TrimSpace(line) == "" {
				continue // header/empty
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			local := fields[1] // ip:port in hex
			st := fields[3]    // state hex
			// local example: 0100007F:1F91
			idx := strings.LastIndex(local, ":")
			if idx < 0 {
				continue
			}
			lp := strings.ToUpper(local[idx+1:])
			if lp != portHex {
				continue
			}
			name := tcpStateName(st)
			stats.Total++
			stats.ByStateName[name]++
		}
	}

	return stats
}

func countOpenFDs() (int, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func readIntFromFile(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func parsePort(s string, def uint16) uint16 {
	if s == "" {
		return def
	}
	p, err := strconv.Atoi(s)
	if err != nil || p <= 0 || p > 65535 {
		return def
	}
	return uint16(p)
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func main() {

	router := gin.Default()

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
		Metrics:     true,
	})
	if err != nil {
		panic(fmt.Errorf("failed to create ristretto cache: %w", err))
	}
	queryCache = cache

	grpcAddr := os.Getenv("LAMINAR_GRPC_ADDR")
	if grpcAddr == "" {
		grpcAddr = "34.177.108.132:50051"
	}
	grpcConn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Errorf("dial %s: %w", grpcAddr, err))
	}
	defer grpcConn.Close()
	grpcClient := pb.NewLaminarGatewayClient(grpcConn)

	debug := os.Getenv("LAMINAR_DEBUG") == "1"
	slowLogMS := envInt("LAMINAR_SLOW_LOG_MS", 250)
	listenPort := parsePort(os.Getenv("LAMINAR_PROXY_PORT"), 8081)

	router.GET("/debug/stats", func(c *gin.Context) {
		fds, fdsErr := countOpenFDs()
		var rlim syscall.Rlimit
		rlErr := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim)
		tcp := countTCPConnsForLocalPort(listenPort)

		// conntrack (if available)
		ctCount, ctCountErr := readIntFromFile("/proc/sys/net/netfilter/nf_conntrack_count")
		ctMax, ctMaxErr := readIntFromFile("/proc/sys/net/netfilter/nf_conntrack_max")

		req := gin.H{
			"now":                  time.Now().Format(time.RFC3339Nano),
			"listen_port":          listenPort,
			"inflight_http":        atomic.LoadInt64(&inflightHTTPRequests),
			"goroutines":           runtime.NumGoroutine(),
			"open_fds":             fds,
			"tcp_local_port_conns": tcp,
			"cache_hits":           atomic.LoadInt64(&cacheHits),
			"cache_misses":         atomic.LoadInt64(&cacheMisses),
			"sf_leader":            atomic.LoadInt64(&sfLeaderCount),
			"sf_shared":            atomic.LoadInt64(&sfSharedCount),
			"grpc_ok":              atomic.LoadInt64(&grpcOKCount),
			"grpc_err":             atomic.LoadInt64(&grpcErrCount),
			"grpc_deadline":        atomic.LoadInt64(&grpcDeadlineCount),
			"grpc_canceled":        atomic.LoadInt64(&grpcCanceledCount),
			"laminar_grpc_addr":    grpcAddr,
		}

		if fdsErr != nil {
			req["open_fds_error"] = fdsErr.Error()
		}
		if rlErr == nil {
			req["rlimit_nofile_cur"] = rlim.Cur
			req["rlimit_nofile_max"] = rlim.Max
		} else {
			req["rlimit_nofile_error"] = rlErr.Error()
		}

		if ctCountErr == nil {
			req["conntrack_count"] = ctCount
		} else {
			req["conntrack_count_error"] = ctCountErr.Error()
		}
		if ctMaxErr == nil {
			req["conntrack_max"] = ctMax
		} else {
			req["conntrack_max_error"] = ctMaxErr.Error()
		}

		// cache internal metrics (best-effort)
		if queryCache != nil {
			m := queryCache.Metrics
			req["ristretto_hits"] = m.Hits()
			req["ristretto_misses"] = m.Misses()
			req["ristretto_keys_added"] = m.KeysAdded()
			req["ristretto_keys_evicted"] = m.KeysEvicted()
			req["ristretto_cost_added"] = m.CostAdded()
			req["ristretto_cost_evicted"] = m.CostEvicted()
		}

		c.JSON(http.StatusOK, req)
	})

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Fast endpoint for QUIC multiplexing tests (small response)
	router.GET("/fast", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.GET("/ping-service-go", func(c *gin.Context) {
		fetchURL := "http://34.177.108.132:8081/api/ping" // Thay đổi URL theo yêu cầu
		resp, err := http.Get(fetchURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch from external host"})
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode response"})
			return
		}

		c.JSON(http.StatusOK, result)
	})

	router.POST("/TestHTTP3-service-go", func(c *gin.Context) {
		var jsonReq struct {
			QueryId  string `json:"QueryId"`
			QuerySQL string `json:"QuerySQL"`
			Payload  string `json:"Payload"`
		}
		if err := c.BindJSON(&jsonReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		addr := "http://34.177.108.132:8081/api/naive"
		payload := map[string]string{
			"QueryId":  jsonReq.QueryId,
			"QuerySQL": jsonReq.QuerySQL,
			"Payload":  jsonReq.Payload,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal payload"})
			return
		}

		resp, err := http.Post(addr, "application/json", bytes.NewBuffer(payloadBytes))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send request to external host"})
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode response"})
			return
		}

		c.JSON(http.StatusOK, result)
	})
	// POST query endpoint (good for load tests; avoids any accidental intermediary caching)
	router.POST("/TestHTTP3", func(c *gin.Context) {
		start := time.Now()
		atomic.AddInt64(&inflightHTTPRequests, 1)
		defer atomic.AddInt64(&inflightHTTPRequests, -1)

		bindStart := time.Now()
		var jsonReq struct {
			QueryId  string `json:"QueryId"`
			QuerySQL string `json:"QuerySQL"`
			Payload  string `json:"Payload"`
		}
		if err := c.BindJSON(&jsonReq); err != nil {
			if debug {
				log.Printf("[/TestHTTP3] bindjson_err=%v bind_dur=%s inflight=%d", err, time.Since(bindStart), atomic.LoadInt64(&inflightHTTPRequests))
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		bindDur := time.Since(bindStart)

		key := jsonReq.QuerySQL
		if key == "" {
			key = jsonReq.QueryId
		}

		// 1) Local cache at gateway (hot responses)
		if val, ok := queryCache.Get(key); ok {
			if cachedResp, ok := val.(*pb.TestHTTP3Response); ok {
				atomic.AddInt64(&cacheHits, 1)
				if debug {
					log.Printf("[/TestHTTP3] cache_hit bind=%s total=%s inflight=%d", bindDur, time.Since(start), atomic.LoadInt64(&inflightHTTPRequests))
				}
				c.JSON(http.StatusOK, gin.H{
					"Status":       cachedResp.GetStatus(),
					"QueryId":      jsonReq.QueryId,
					"Records":      cachedResp.GetRecords(),
					"ReceivedSize": cachedResp.GetReceivedSize(),
				})
				return
			}
		}
		atomic.AddInt64(&cacheMisses, 1)

		doStart := time.Now()
		resAny, err, shared := testHTTP3SingleFlight.Do(key, func() (interface{}, error) {
			// Double-check cache inside singleflight to avoid duplicate work
			if val, ok := queryCache.Get(key); ok {
				if cachedResp, ok := val.(*pb.TestHTTP3Response); ok {
					return cachedResp, nil
				}
			}

			ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
			defer cancel()
			payload := make([]byte, 10)
			grpcStart := time.Now()
			resp, err := grpcClient.TestHTTP3(ctx, &pb.TestHTTP3Request{
				QueryId:  jsonReq.QueryId,
				QuerySQL: jsonReq.QuerySQL,
				Payload:  payload,
			})
			if err != nil {
				atomic.AddInt64(&grpcErrCount, 1)
				if errors.Is(err, context.DeadlineExceeded) {
					atomic.AddInt64(&grpcDeadlineCount, 1)
				}
				if errors.Is(err, context.Canceled) {
					atomic.AddInt64(&grpcCanceledCount, 1)
				}
				if debug {
					log.Printf("[/TestHTTP3] grpc_err=%v grpc_dur=%s", err, time.Since(grpcStart))
				}
				return nil, err
			}
			atomic.AddInt64(&grpcOKCount, 1)
			// 2) Store into gateway cache (TTL 20s)
			queryCache.SetWithTTL(key, resp, 1, 20*time.Second)
			if debug {
				log.Printf("[/TestHTTP3] grpc_ok grpc_dur=%s", time.Since(grpcStart))
			}
			return resp, nil
		})
		if shared {
			atomic.AddInt64(&sfSharedCount, 1)
		} else {
			atomic.AddInt64(&sfLeaderCount, 1)
		}
		doDur := time.Since(doStart)

		if err != nil {
			if doDur >= time.Duration(slowLogMS)*time.Millisecond || debug {
				log.Printf("[/TestHTTP3] sf_done_err=%v do=%s bind=%s total=%s shared=%t inflight=%d", err, doDur, bindDur, time.Since(start), shared, atomic.LoadInt64(&inflightHTTPRequests))
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("TestHTTP3: %v", err)})
			return
		}

		resp := resAny.(*pb.TestHTTP3Response)
		if doDur >= time.Duration(slowLogMS)*time.Millisecond || debug {
			log.Printf("[/TestHTTP3] sf_done_ok do=%s bind=%s total=%s shared=%t inflight=%d", doDur, bindDur, time.Since(start), shared, atomic.LoadInt64(&inflightHTTPRequests))
		}
		// Preserve per-request QueryId even when coalesced.
		c.JSON(http.StatusOK, gin.H{
			"Status":       resp.GetStatus(),
			"QueryId":      jsonReq.QueryId,
			"Records":      resp.GetRecords(),
			"ReceivedSize": resp.GetReceivedSize(),
		})

	})

	port := os.Getenv("LAMINAR_PROXY_PORT")
	if port == "" {
		port = "8081"
	}
	// ensure listenPort used by /debug/stats matches actual listen port
	listenPort = parsePort(port, 8081)
	fmt.Printf("Starting server on :%s\n", port)

	router.Run("0.0.0.0:" + port) // listen and serve
}
