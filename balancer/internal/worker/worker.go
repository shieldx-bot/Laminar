package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	cgvnode "github/shieldx-bot/loadbanlacing/internal/cg-vnode"
	pb "github/shieldx-bot/loadbanlacing/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type VPS struct {
	IP     string
	caches []ListCachesOnServer
}

type ListCachesOnServer struct {
	Timestamp time.Time
	queryKey  string
}

// Danh sách địa chỉ gRPC của các Worker
var ListVPS = []VPS{
	{IP: "localhost:50051", caches: []ListCachesOnServer{}},
	{IP: "localhost:50052", caches: []ListCachesOnServer{}},
	{IP: "localhost:50053", caches: []ListCachesOnServer{}},
	{IP: "localhost:50054", caches: []ListCachesOnServer{}},
	{IP: "localhost:50055", caches: []ListCachesOnServer{}},
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

func HasCacheInVPS(queryKey string) []string {
	// Logic cache đơn giản, giữ nguyên hoặc tối ưu sau
	listVPSMu.Lock()
	defer listVPSMu.Unlock()
	// ... (giữ nguyên logic cache hit giả lập - có thể implement lại sau nếu cần)
	return []string{}
}

type ComputeServer struct {
	pb.UnimplementedLaminarGatewayServer
}

// Pool kết nối gRPC
var (
	grpcConns   = make(map[string]*grpc.ClientConn)
	grpcConnsMu sync.RWMutex
)

func getGrpcConn(addr string) (*grpc.ClientConn, error) {
	grpcConnsMu.RLock()
	conn, ok := grpcConns[addr]
	grpcConnsMu.RUnlock()
	if ok {
		return conn, nil
	}

	grpcConnsMu.Lock()
	defer grpcConnsMu.Unlock()
	// Double check
	if conn, ok := grpcConns[addr]; ok {
		return conn, nil
	}

	// Dial gRPC node
	conn, err := grpc.Dial(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// grpc.WithBlock(), // Bỏ WithBlock để non-blocking dial
	)
	if err != nil {
		return nil, err
	}
	grpcConns[addr] = conn
	return conn, nil
}

// ==== =============== HASHING VNODE & GET NODE ================

func GetNode(key string) []string {
	listVPSMu.RLock()
	listIPs := make([]string, 0, len(ListVPS))
	for _, v := range ListVPS {
		listIPs = append(listIPs, v.IP)
	}
	listVPSMu.RUnlock()

	ring := cgvnode.NewHashRing(listIPs, 100)
	selected := ring.GetNode(key)

	result := make([]string, 0, len(selected))
	for _, n := range selected {
		result = append(result, n.Key)
	}
	return result
}

func NewComputeServer() *ComputeServer {
	return &ComputeServer{}
}

func (s *ComputeServer) SubmitJob(ctx context.Context, req *pb.RequestToBalancer) (*pb.ResponseToBalancer, error) {
	// 1. Dinh tuyen
	nodes := GetNode(req.QuerySQL)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no available backend nodes")
	}

	// 2. Gửi request gRPC song song (Fire and Forget)
	go func(targetNodes []string, r *pb.RequestToBalancer) {
		for _, addr := range targetNodes {
			conn, err := getGrpcConn(addr)
			if err != nil {
				log.Printf("Worker: Failed to get conn for %s: %v", addr, err)
				continue
			}

			client := pb.NewLaminarGatewayClient(conn)

			// Call gRPC Method
			// Timeout ngắn cho call
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = client.TestHTTP3(ctx, r)
			cancel()

			if err != nil {
				log.Printf("Worker: gRPC to %s failed: %v", addr, err)
			}
		}
	}(nodes, req)

	return &pb.ResponseToBalancer{Status: "202 Accepted"}, nil
}
