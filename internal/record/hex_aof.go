package record

import (
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// CreateOrOpenAOF opens the file at the given path in append mode, creating it if it doesn't exist.
func CreateOrOpenAOF(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
}

// LogPacketAOF logs a single line in CSV format: timestamp,size,packetType,encoded(pkt)
func LogPacketAOF(packetType int, pkt []byte, file *os.File) error {
	timestamp := time.Now().UTC().Format(time.RFC3339)
	size := len(pkt)
	encoded := hex.EncodeToString(pkt)
	line := fmt.Sprintf("%s,%d,%d,%s\n", timestamp, size, packetType, encoded)
	_, err := file.WriteString(line)
	return err
}
