package models

import "time"

// Offset tracks how far a log-client has read into a file.
// Primary key: (MachineID, FilePath).
type Offset struct {
	MachineID string    `db:"machine_id"`
	FilePath  string    `db:"file_path"`
	Offset    int64     `db:"offset"`
	UpdatedAt time.Time `db:"updated_at"`
}

// Log is one stored log entry pushed by a client.
type Log struct {
	ID        int64     `db:"id"`
	MachineID string    `db:"machine_id"`
	FilePath  string    `db:"file_path"`
	StartTime time.Time `db:"start_time"`
	Level     LogLevel  `db:"level"`
	Message   string    `db:"message"`
}

// LogLevel mirrors the log levels visible in the diagram.
type LogLevel string

const (
	LevelDebug   LogLevel = "DEBUG"
	LevelInfo    LogLevel = "INFO"
	LevelWarning LogLevel = "WARNING"
	LevelError   LogLevel = "ERROR"
	LevelFatal   LogLevel = "FATAL"
)



