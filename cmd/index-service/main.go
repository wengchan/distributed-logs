package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	pb "distributed-logs/proto/indexservice"
	"distributed-logs/internal/server"
)

func main() {
	addr := ":50051"

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}

	grpcServer := grpc.NewServer()

	indexSvc := server.NewIndexServiceServer()
	pb.RegisterIndexServiceServer(grpcServer, indexSvc)

	log.Printf("index-service listening at %s", addr)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}