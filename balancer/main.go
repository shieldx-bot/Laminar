package main

import (
	"fmt"
	wk "github/shieldx-bot/loadbanlacing/internal/worker"
	"github/shieldx-bot/loadbanlacing/pb"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/rs/cors"
)

type server struct {
	pb.UnimplementedLaminarGatewayServer
	cs *wk.ComputeServer
}

func NewServer(cs *wk.ComputeServer) *server {
	return &server{
		cs: cs,
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, proceeding with environment variables")
	}
	computeServer := wk.NewComputeServer()
	_ = computeServer
	Myserver := NewServer(computeServer)
	_ = Myserver

	router := gin.Default()

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, "Pong from Load Balancer")
	})

	router.POST("/balance", func(c *gin.Context) {
		var req pb.RequestToBalancer
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		res, err := computeServer.SubmitJob(c.Request.Context(), &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Trả về kết quả thực tế thay vì chuỗi sring "res"
		// res là *pb.ResponseToBalancer có trường Status
		c.JSON(http.StatusOK, res)
	})
	var port = os.Getenv("LAMINAR_DB_PORT")
	if port == "" {
		port = "8083" // Default port if not set
	}

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://bar.com", "*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Content-Length"},
		AllowCredentials: true,
		// Enable Debugging for testing, consider disabling in production
		Debug: true,
	})

	// Use the Gin router as the handler, wrapped with CORS
	handler := c.Handler(router)

	fmt.Printf("Load Balancer running on :%s\n", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
