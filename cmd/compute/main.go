package main

import (
	"context"
	pb "github/shieldx-bot/laminar/pb"
	"log"
	"net"

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedLaminarGatewayServer
}

func (s *server) ProcessSingle(ctx context.Context, in *pb.WorkRequest) (*pb.WorkResponse, error) {
	// Implement your logic here
	return &pb.WorkResponse{RequestId: "Processed: " + in.RequestId}, nil
}

func main() {
	// Server setup code would go here
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		panic(err)
	}
	s := grpc.NewServer()
	pb.RegisterLaminarGatewayServer(s, &server{})
	if err := s.Serve(listener); err != nil {
		panic(err)
	}

	log.Println("Server is running on port :50051")
	s.Serve(listener)
}
