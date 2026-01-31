package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

var listingLogDir = "."

// InitListingLog sets the directory for listing logs.
func InitListingLog(dir string) error {
	if dir != "" {
		listingLogDir = dir
	}
	return os.MkdirAll(listingLogDir, 0755)
}

// getLogPath returns the log file path for a user.
func getLogPath(userID int64) string {
	return filepath.Join(listingLogDir, fmt.Sprintf("listing_%d.log", userID))
}

// StartListingLog truncates the log file for a user, starting a fresh log.
func StartListingLog(userID int64) {
	logPath := getLogPath(userID)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Error().Err(err).Int64("userID", userID).Msg("failed to start listing log")
		return
	}
	defer f.Close()

	header := fmt.Sprintf("=== Listing Log ===\nUser: %d\nStarted: %s\n\n",
		userID, time.Now().Format("2006-01-02 15:04:05"))
	f.WriteString(header)
}

// appendLog writes a log entry to the user's listing log file.
func appendLog(userID int64, prefix, msg string) {
	f, err := os.OpenFile(getLogPath(userID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Error().Err(err).Int64("userID", userID).Msg("failed to write listing log")
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] %s %s\n", timestamp, prefix, msg)
	f.WriteString(line)
}

// LogUser logs a user message/action.
func LogUser(userID int64, format string, args ...any) {
	appendLog(userID, "USER    ", fmt.Sprintf(format, args...))
}

// LogBot logs a bot response.
func LogBot(userID int64, format string, args ...any) {
	appendLog(userID, "BOT     ", fmt.Sprintf(format, args...))
}

// LogInternal logs internal processing.
func LogInternal(userID int64, format string, args ...any) {
	appendLog(userID, "INTERNAL", fmt.Sprintf(format, args...))
}

// LogAPI logs API calls.
func LogAPI(userID int64, format string, args ...any) {
	appendLog(userID, "API     ", fmt.Sprintf(format, args...))
}

// LogState logs state transitions.
func LogState(userID int64, format string, args ...any) {
	appendLog(userID, "STATE   ", fmt.Sprintf(format, args...))
}

// LogError logs errors.
func LogError(userID int64, format string, args ...any) {
	appendLog(userID, "ERROR   ", fmt.Sprintf(format, args...))
}

// LogCallback logs callback events.
func LogCallback(userID int64, format string, args ...any) {
	appendLog(userID, "CALLBACK", fmt.Sprintf(format, args...))
}

// LogLLM logs LLM interactions.
func LogLLM(userID int64, format string, args ...any) {
	appendLog(userID, "LLM     ", fmt.Sprintf(format, args...))
}
