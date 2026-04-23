package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "distributed-logs/proto/indexservice"
)

func main() {
	serverAddr   := flag.String("server", "localhost:50051", "gRPC server address")
	machineID    := flag.String("machine_id", "machine-001", "machine id")
	dirPath      := flag.String("path", ".", "directory containing log files")
	interval     := flag.Duration("interval", 10*time.Second, "scan interval")
	summarizeAddr := flag.String("summarize-addr", "", "summarize-service address e.g. localhost:8081 (optional)")
	flag.Parse()

	conn, err := grpc.NewClient(
		*serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to create gRPC client: %v", err)
	}
	defer conn.Close()

	client := pb.NewIndexServiceClient(conn)

	log.Printf("log-client started: server=%s machine_id=%s path=%s interval=%s summarize=%s",
		*serverAddr, *machineID, *dirPath, *interval, *summarizeAddr)

	scanAndProcess(client, *machineID, *dirPath, *summarizeAddr)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for range ticker.C {
		scanAndProcess(client, *machineID, *dirPath, *summarizeAddr)
	}
}

func scanAndProcess(client pb.IndexServiceClient, machineID, dirPath, summarizeAddr string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		log.Printf("failed to read dir %s: %v", dirPath, err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dirPath, entry.Name())
		processFile(client, machineID, fullPath, summarizeAddr)
	}
}

func processFile(client pb.IndexServiceClient, machineID, filePath, summarizeAddr string) {
	ctx1, cancel1 := context.WithTimeout(context.Background(), 3*time.Second)
	getResp, err := client.GetOffset(ctx1, &pb.GetOffsetRequest{
		MachineId: machineID,
		FilePath:  filePath,
	})
	cancel1()
	if err != nil {
		log.Printf("GetOffset failed for %s: %v", filePath, err)
		return
	}

	offset := getResp.GetOffset()
	log.Printf("GetOffset: file=%s offset=%d", filePath, offset)

	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("failed to open file %s: %v", filePath, err)
		return
	}
	defer f.Close()

	newPos, err := f.Seek(offset, io.SeekStart)
	if err != nil {
		log.Printf("failed to seek file %s to offset %d: %v", filePath, offset, err)
		return
	}

	var logLines []string
	reader := bufio.NewScanner(f)
	for reader.Scan() {
		logLines = append(logLines, reader.Text())
	}
	if err := reader.Err(); err != nil {
		log.Printf("failed while reading file %s: %v", filePath, err)
		return
	}

	endOffset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		log.Printf("failed to get current offset for file %s: %v", filePath, err)
		return
	}

	if len(logLines) == 0 {
		log.Printf("no new logs for file=%s at offset=%d", filePath, newPos)
		return
	}

	log.Printf("read %d new lines from file=%s start=%d end=%d",
		len(logLines), filePath, newPos, endOffset)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	pushResp, err := client.PushLogs(ctx2, &pb.PushLogsRequest{
		MachineId:   machineID,
		FilePath:    filePath,
		StartOffset: newPos,
		EndOffset:   endOffset,
		LogLines:    logLines,
	})
	cancel2()
	if err != nil {
		log.Printf("PushLogs failed for %s: %v", filePath, err)
		return
	}

	log.Printf("PushLogs success: file=%s status=%v message=%s",
		filePath, pushResp.GetStatus(), pushResp.GetMessage())

	// Optionally call the summarize service in the background.
	if summarizeAddr != "" {
		go requestSummary(summarizeAddr, machineID, filePath, logLines)
	}
}

// requestSummary posts log lines to the summarize service and logs the result.
// Runs in a goroutine so it never blocks the main push loop.
func requestSummary(addr, machineID, filePath string, logLines []string) {
	body, err := json.Marshal(map[string]any{
		"machine_id": machineID,
		"file_path":  filePath,
		"log_lines":  logLines,
	})
	if err != nil {
		log.Printf("summarize marshal error: %v", err)
		return
	}

	url := fmt.Sprintf("http://%s/summarize", addr)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("summarize request error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("summarize call failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Summary  string `json:"summary"`
		LogCount int    `json:"log_count"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("summarize decode error: %v", err)
		return
	}
	if result.Error != "" {
		log.Printf("summarize error: %s", result.Error)
		return
	}

	log.Printf("=== SUMMARY [%s] %d lines ===\n%s\n=== END SUMMARY ===",
		filePath, result.LogCount, result.Summary)
}
