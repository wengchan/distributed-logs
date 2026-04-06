package server

import (
	"context"
	"log"

	pb "distributed-logs/proto/indexservice"
)

// IndexServiceServer implements the gRPC service defined in proto.
type IndexServiceServer struct {
	pb.UnimplementedIndexServiceServer
}

// NewIndexServiceServer creates a new dummy index service server.
func NewIndexServiceServer() *IndexServiceServer {
	return &IndexServiceServer{}
}

// GetOffset returns a dummy offset for now.
func (s *IndexServiceServer) GetOffset(
	ctx context.Context,
	req *pb.GetOffsetRequest,
) (*pb.GetOffsetResponse, error) {
	log.Printf("GetOffset called: machine_id=%s file_path=%s", req.GetMachineId(), req.GetFilePath())

	return &pb.GetOffsetResponse{
		Status:  pb.Status_STATUS_OK,
		Offset:  0,
		Message: "dummy offset response",
	}, nil
}

// PushLogs accepts pushed logs and returns a dummy success response.
func (s *IndexServiceServer) PushLogs(
	ctx context.Context,
	req *pb.PushLogsRequest,
) (*pb.PushLogsResponse, error) {
	log.Printf(
		"PushLogs called: machine_id=%s file_path=%s start=%d end=%d lines=%d",
		req.GetMachineId(),
		req.GetFilePath(),
		req.GetStartOffset(),
		req.GetEndOffset(),
		len(req.GetLogLines()),
	)

	return &pb.PushLogsResponse{
		Status:  pb.Status_STATUS_OK,
		Message: "dummy push success",
	}, nil
}