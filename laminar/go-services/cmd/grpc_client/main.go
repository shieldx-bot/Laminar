package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github/shieldx-bot/laminar/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := "localhost:50051"

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()

	client := pb.NewLaminarGatewayClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.PingPong(ctx, &pb.PingRequest{Message: "Hello, Laminar!"})
	if err != nil {
		log.Fatalf("PingPong: %v", err)
	}

	fmt.Printf("PingPong response: %s\n", resp.GetMessage())
}
