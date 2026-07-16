package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Event is one parsed JSON line emitted by a shim on stdout (see
// interop/clients/go/main.go's doc comment for the full event vocabulary shared by
// all four shims).
type Event map[string]any

func (e Event) Type() string {
	s, _ := e["event"].(string)
	return s
}

func (e Event) Str(key string) string {
	s, _ := e[key].(string)
	return s
}

func (e Event) Bool(key string) bool {
	b, _ := e[key].(bool)
	return b
}

// SDKDef describes how to invoke one language's CLI shim.
type SDKDef struct {
	Name    string
	Command string
	Args    []string // prepended before the shared --server/--device-id/... flags
}

// Instance is a running shim subprocess, driven over stdin/stdout.
type Instance struct {
	Name string

	cmd   *exec.Cmd
	stdin io.WriteCloser
	raw   chan Event

	mu      sync.Mutex
	backlog []Event
}

// StartInstance launches a shim and blocks until it reports "ready" (connected and
// authenticated) or "connect_error".
func StartInstance(def SDKDef, serverURL, deviceID, deviceToken, peerStorePath string) (*Instance, error) {
	args := make([]string, 0, len(def.Args)+8)
	args = append(args, def.Args...)
	args = append(args, "--server", serverURL, "--device-id", deviceID, "--device-token", deviceToken)
	if peerStorePath != "" {
		args = append(args, "--peer-store-path", peerStorePath)
	}

	cmd := exec.Command(def.Command, args...) //nolint:gosec // fixed set of local dev-toolchain binaries, not user input
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: stdin pipe: %w", def.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: stdout pipe: %w", def.Name, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: start: %w", def.Name, err)
	}

	inst := &Instance{Name: def.Name, cmd: cmd, stdin: stdin, raw: make(chan Event, 256)}
	go inst.readLoop(stdout)

	ev, err := inst.WaitFor(15*time.Second, func(e Event) bool {
		return e.Type() == "ready" || e.Type() == "connect_error"
	})
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%s: %w", def.Name, err)
	}
	if ev.Type() == "connect_error" {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%s: connect_error: %s", def.Name, ev.Str("message"))
	}
	return inst, nil
}

func (i *Instance) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		i.raw <- ev
	}
	close(i.raw)
}

// Send writes one JSON command to the shim's stdin.
func (i *Instance) Send(cmd map[string]any) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	_, err = i.stdin.Write(append(data, '\n'))
	return err
}

// WaitFor blocks until an event matching pred arrives (checking already-buffered
// events first, so scenarios can wait for different event types out of order without
// losing intervening events — e.g. a "message" arriving while waiting for "paired").
func (i *Instance) WaitFor(timeout time.Duration, pred func(Event) bool) (Event, error) {
	i.mu.Lock()
	for idx, ev := range i.backlog {
		if pred(ev) {
			i.backlog = append(i.backlog[:idx], i.backlog[idx+1:]...)
			i.mu.Unlock()
			return ev, nil
		}
	}
	i.mu.Unlock()

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("%s: timed out waiting for event", i.Name)
		}
		select {
		case ev, ok := <-i.raw:
			if !ok {
				return nil, fmt.Errorf("%s: event stream closed before matching event", i.Name)
			}
			if pred(ev) {
				return ev, nil
			}
			i.mu.Lock()
			i.backlog = append(i.backlog, ev)
			i.mu.Unlock()
		case <-time.After(remaining):
			return nil, fmt.Errorf("%s: timed out waiting for event", i.Name)
		}
	}
}

// Close asks the shim to close gracefully, then kills the process if it doesn't
// exit promptly.
func (i *Instance) Close() {
	_ = i.Send(map[string]any{"cmd": "close"})
	done := make(chan struct{})
	go func() {
		_ = i.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = i.cmd.Process.Kill()
		<-done
	}
}
