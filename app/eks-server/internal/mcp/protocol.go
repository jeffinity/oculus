package mcp

import (
	"encoding/json"
	"io"
	"sync"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type                 string                    `json:"type"`
	Properties           map[string]schemaProperty `json:"properties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type schemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type commandArgs struct {
	Cluster        string  `json:"cluster"`
	Env            string  `json:"env"`
	Command        string  `json:"command"`
	TimeoutSeconds float64 `json:"timeout_seconds,omitempty"`
}

type safeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newSafeWriter(w io.Writer) *safeWriter {
	return &safeWriter{w: w}
}

func (w *safeWriter) write(resp response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(append(data, '\n'))
	return err
}

func newError(code int, msg string) *responseError {
	return &responseError{Code: code, Message: msg}
}
