package logparse

import (
	"regexp"
	"time"

	"distributed-logs/models"
)

// Pattern matches lines like:
//
//	2026-04-05 10:33:23 INFO  Server is starting
//	2026-04-05 10:33:23 ERROR something went wrong
var lineRe = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+(DEBUG|INFO|WARNING|ERROR|FATAL)\s+(.+)$`,
)

const tsLayout = "2006-01-02 15:04:05"

// ParseLine tries to parse a structured log line.
// Returns (log, true) on success, or a best-effort Log with level=INFO and the
// raw text as the message when the line does not match the expected format.
func ParseLine(machineID, filePath, raw string) (models.Log, bool) {
	m := lineRe.FindStringSubmatch(raw)
	if m == nil {
		return models.Log{
			MachineID: machineID,
			FilePath:  filePath,
			StartTime: time.Now().UTC(),
			Level:     models.LevelInfo,
			Message:   raw,
		}, false
	}

	ts, err := time.ParseInLocation(tsLayout, m[1], time.UTC)
	if err != nil {
		ts = time.Now().UTC()
	}

	return models.Log{
		MachineID: machineID,
		FilePath:  filePath,
		StartTime: ts,
		Level:     models.LogLevel(m[2]),
		Message:   m[3],
	}, true
}

// ParseLines converts a slice of raw strings into Log entries.
// Every line produces exactly one Log — unstructured lines are kept with level INFO.
func ParseLines(machineID, filePath string, lines []string) []models.Log {
	logs := make([]models.Log, 0, len(lines))
	for _, l := range lines {
		entry, _ := ParseLine(machineID, filePath, l)
		logs = append(logs, entry)
	}
	return logs
}
