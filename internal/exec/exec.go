// Package exec provides safe process spawning helpers, including the
// stdio JSON-RPC client used to talk to `codex app-server`.
package exec

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// RPCRequest is a JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// RPCResponse is a JSON-RPC 2.0 response (notifications have Method set).
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is the JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPCLog is a single received message (used to surface unexpected input).
type RPCLog struct {
	Method string
	ID     int64
	Body   string
}

// RPCClient is a minimal JSON-RPC 2.0 client over a process's stdio.
type RPCClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	sc     *bufio.Scanner
	logs   []RPCLog
	mu     sync.Mutex
	cancel context.CancelFunc
}

// StartRPC launches `name args...` and prepares the JSON-RPC transport.
func StartRPC(ctx context.Context, name string, args ...string) (*RPCClient, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &RPCClient{
		cmd:   cmd,
		stdin: stdin,
		sc:    bufio.NewScanner(stdout),
	}
	c.sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return c, nil
}

// Call sends a request with the given ID and waits for the response with that ID.
func (c *RPCClient) Call(ctx context.Context, id int64, method string, params any) (*RPCResponse, error) {
	if err := c.send(method, id, params); err != nil {
		return nil, err
	}
	return c.waitFor(ctx, id)
}

// Notify sends a notification (no id) and does not wait for a response.
func (c *RPCClient) Notify(ctx context.Context, method string, params any) error {
	return c.send(method, 0, params)
}

func (c *RPCClient) send(method string, id int64, params any) error {
	req := RPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// waitFor scans stdout until a response with the target ID arrives.
// Notifications (no id, method present) are recorded and skipped.
func (c *RPCClient) waitFor(ctx context.Context, want int64) (*RPCResponse, error) {
	resCh := make(chan *RPCResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		for c.sc.Scan() {
			line := c.sc.Bytes()
			var resp RPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue // ignore junk lines
			}
			// Notifications carry no id (unmarshals to 0) but always have a
			// method; responses never do. Matching on method prevents a
			// notification (e.g. emitted right after initialize, which uses
			// id 0) from being mistaken for a response.
			if resp.Method == "" && resp.ID == want {
				resCh <- &resp
				return
			}
			c.mu.Lock()
			if len(c.logs) < 200 {
				c.logs = append(c.logs, RPCLog{Method: resp.Method, ID: resp.ID, Body: string(line)})
			}
			c.mu.Unlock()
		}
		if err := c.sc.Err(); err != nil {
			errCh <- err
		} else {
			errCh <- errors.New("rpc: connection closed before response")
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case resp := <-resCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	}
}

// Logs returns received notifications/junk (for diagnostics).
func (c *RPCClient) Logs() []RPCLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]RPCLog, len(c.logs))
	copy(out, c.logs)
	return out
}

// Close terminates the child process.
func (c *RPCClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = c.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = c.cmd.Process.Kill()
		}
	}
}
