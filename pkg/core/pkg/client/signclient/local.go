//go:build x402

package signclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

const maxResponseBytes = 16 * 1024

type LocalTransport struct {
	socketPath string
	timeout    time.Duration
}

func NewLocalTransport(socketPath string, timeout time.Duration) (*LocalTransport, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("wallet local transport requires wallet.local_socket")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &LocalTransport{socketPath: socketPath, timeout: timeout}, nil
}

func (t *LocalTransport) RoundTrip(ctx context.Context, request *SignRequest) (*SignResponse, error) {
	dialer := net.Dialer{Timeout: t.timeout}
	connection, err := dialer.DialContext(ctx, "unix", t.socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to wallet socket: %w", err)
	}
	defer connection.Close()
	deadline := time.Now().Add(t.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set wallet socket deadline: %w", err)
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return nil, fmt.Errorf("send wallet signing request: %w", err)
	}

	reader := bufio.NewReader(io.LimitReader(connection, maxResponseBytes+1))
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read wallet signing response: %w", err)
	}
	if len(line) > maxResponseBytes {
		return nil, fmt.Errorf("wallet signing response exceeds %d bytes", maxResponseBytes)
	}
	var response SignResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("decode wallet signing response: %w", err)
	}
	return &response, nil
}

var _ Transport = (*LocalTransport)(nil)
