package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	wk "github/shieldx-bot/laminar/internal/worker"
	pb "github/shieldx-bot/laminar/pb"

	_ "github.com/lib/pq" // Driver postgres
	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedLaminarGatewayServer
	db *sql.DB // 1. Thêm field này để tái sử dụng DB Pool
	cs *wk.ComputeServer
}

// Hàm khởi tạo Server mới, nhận DB từ bên ngoài vào
func NewServer(db *sql.DB, cs *wk.ComputeServer) *server {
	return &server{
		db: db,
		cs: cs,
	}
}

func (s *server) PingPong(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	fmt.Println("Received Ping:", req.Message)
	return &pb.PingResponse{Message: "Pong"}, nil
}

func (s *server) TestHTTP3(ctx context.Context, req *pb.TestHTTP3Request) (*pb.TestHTTP3Response, error) {
	// 4. Ở đây bạn có thể dùng s.db để query DB thoải mái
	// Ví dụ: s.db.QueryContext(ctx, "SELECT 1")

	// Placeholder implementation
	req = &pb.TestHTTP3Request{
		QueryId:  req.QueryId,
		QuerySQL: req.QuerySQL,
		Payload:  req.Payload,
	}

	res, err := s.cs.ExecuteQuery(context.Background(), req)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Request Test HTTP3 Đã Nhận Và Sử Lý Thành Công !\n")
	return &pb.TestHTTP3Response{
		Status:       res.Status,
		QueryId:      res.QueryId,
		Records:      res.Records,
		ReceivedSize: res.ReceivedSize,
	}, nil

}

func main() {
	// 2. KHỞI TẠO KẾT NỐI DB MỘT LẦN DUY NHẤT LÚC STARTUP
	connStr := "host=34.177.108.132 port=5432 user=postgres password=Vananh12345@ dbname=laminar sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic(err)
	}
	// Đừng đóng DB ngay, chỉ đóng khi main exit
	defer db.Close()

	// Cấu hình Connection Pool (Quan trọng cho High Performance)
	db.SetMaxOpenConns(200)  // Giới hạn max 1000 kết nối cùng lúc
	db.SetMaxIdleConns(25)   // Giữ 25 kết nối rảnh để dùng ngay
	db.SetConnMaxLifetime(0) // 0 = dùng mãi mãi (hoặc set time để refresh)

	// Ping kiểm tra
	if err := db.Ping(); err != nil {
		fmt.Println("DB Fail:", err)
		// Có thể return hoặc panic tùy chiến lược
	} else {
		fmt.Println("Connected to DB successfully")
	}

	// 2.5 KHỞI TẠO COMPUTE SERVER (WORKER POOL) MỘT LẦN
	computeServer := wk.NewComputeServer(db)

	// Start mảng mạng
	list, err := net.Listen("tcp", ":50051")
	if err != nil {
		fmt.Println("Failed to listen:", err)
		return
	}

	grpcServer := grpc.NewServer()

	// 3. TRUYỀN DB VÀ COMPUTE SERVER VÀO GATEWAY
	myServer := NewServer(db, computeServer)
	pb.RegisterLaminarGatewayServer(grpcServer, myServer)

	fmt.Println("gRPC server listening on :50051")
	if err := grpcServer.Serve(list); err != nil {
		fmt.Println("Failed to serve:", err)
	}
}
