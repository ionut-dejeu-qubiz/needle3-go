package engine

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// The worker wire protocol is an 8-byte big-endian payload length
// followed by a UTF-8 JSON object.

const headerSize = 8

// workerConfig is the first message the parent sends to a child.
type workerConfig struct {
	Library    string `json:"library"`              // absolute path to libneedle.*
	Weights    string `json:"weights"`              // absolute path to the .cact
	System     string `json:"system"`               // system facts text
	Tools      string `json:"tools"`                // tools JSON array
	ToolIndex  string `json:"tool_index,omitempty"` // optional tool embedding path
	BufferSize int    `json:"buffer_size"`          // response buffer size
	Generation int    `json:"generation"`           // engine generation
}

// workerRequest is one operation sent to the child.
type workerRequest struct {
	Operation    string `json:"operation"` // "complete" | "reset" | "close"
	Text         string `json:"text,omitempty"`
	MaxNewTokens int    `json:"max_new_tokens,omitempty"`
}

// workerResponse is every message the child sends back.
type workerResponse struct {
	Status       string `json:"status"`             // "ready" | "ok" | "error" | "fatal"
	Message      string `json:"message,omitempty"`  // set on error/fatal
	Response     string `json:"response,omitempty"` // raw JSON envelope on complete
	PrefixTokens int    `json:"prefix_tokens,omitempty"`
}

// writeMessage writes one length-prefixed JSON message.
func writeMessage(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var header [headerSize]byte
	binary.BigEndian.PutUint64(header[:], uint64(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

// readMessage reads one length-prefixed JSON message. It returns
// io.EOF-ish nil when the stream closed cleanly before a header.
func readMessage(r io.Reader, v any) error {
	var header [headerSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if err == io.EOF {
			return io.EOF
		}
		return fmt.Errorf("needle: truncated worker message: %w", err)
	}
	size := binary.BigEndian.Uint64(header[:])
	if size == 0 || size > 1<<31 {
		return fmt.Errorf("needle: invalid worker message length %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return fmt.Errorf("needle: truncated worker message: %w", err)
	}
	return json.Unmarshal(payload, v)
}
