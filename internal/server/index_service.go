package server

import (
	"context"
	"log"

	pb "distributed-logs/proto/indexservice"
	"distributed-logs/internal/db"
	"distributed-logs/internal/logparse"
)

// IndexServiceServer implements the gRPC service defined in proto.
type IndexServiceServer struct {
	pb.UnimplementedIndexServiceServer
	db *db.DB
}

// NewIndexServiceServer creates a server backed by the given DB.
func NewIndexServiceServer(database *db.DB) *IndexServiceServer {
	return &IndexServiceServer{db: database}
}

// GetOffset returns the saved byte offset for a (machine, file) pair.
func (s *IndexServiceServer) GetOffset(
	ctx context.Context,
	req *pb.GetOffsetRequest,
) (*pb.GetOffsetResponse, error) {
	offset, err := s.db.GetOffset(ctx, req.GetMachineId(), req.GetFilePath())
	if err != nil {
		log.Printf("GetOffset db error: machine_id=%s file=%s err=%v", req.GetMachineId(), req.GetFilePath(), err)
		return &pb.GetOffsetResponse{Status: pb.Status_STATUS_ERROR, Message: err.Error()}, nil
	}

	log.Printf("GetOffset: machine_id=%s file=%s offset=%d", req.GetMachineId(), req.GetFilePath(), offset)
	return &pb.GetOffsetResponse{Status: pb.Status_STATUS_OK, Offset: offset}, nil
}

// PushLogs stores incoming log lines and advances the saved offset.
func (s *IndexServiceServer) PushLogs(
	ctx context.Context,
	req *pb.PushLogsRequest,
) (*pb.PushLogsResponse, error) {
	machineID := req.GetMachineId()
	filePath := req.GetFilePath()

	logs := logparse.ParseLines(machineID, filePath, req.GetLogLines())

	if err := s.db.InsertLogs(ctx, logs); err != nil {
		log.Printf("PushLogs InsertLogs error: machine_id=%s file=%s err=%v", machineID, filePath, err)
		return &pb.PushLogsResponse{Status: pb.Status_STATUS_ERROR, Message: err.Error()}, nil
	}

	if err := s.db.UpsertOffset(ctx, machineID, filePath, req.GetEndOffset()); err != nil {
		log.Printf("PushLogs UpsertOffset error: machine_id=%s file=%s err=%v", machineID, filePath, err)
		return &pb.PushLogsResponse{Status: pb.Status_STATUS_ERROR, Message: err.Error()}, nil
	}

	log.Printf("PushLogs ok: machine_id=%s file=%s lines=%d end_offset=%d",
		machineID, filePath, len(logs), req.GetEndOffset())
	return &pb.PushLogsResponse{Status: pb.Status_STATUS_OK}, nil
}
