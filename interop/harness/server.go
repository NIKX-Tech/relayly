package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// repoRoot is derived from this source file's own location rather than the caller's
// working directory: interop/harness is its own Go module (like sdk/go), so it must
// be run as `cd interop/harness && go run .`, not `go run ./interop/harness` from the
// repo root (that only works with a local go.work file unifying modules, which is
// gitignored — a personal dev convenience, not something CI or a fresh clone has).
// runtime.Caller(0) keeps this correct regardless of the cwd the harness is launched
// from.
func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("could not determine source file location")
	}
	// this file is at <root>/interop/harness/server.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("could not locate repository root from %s: %w", thisFile, err)
	}
	return root, nil
}

func buildServerBinary(root, outDir string) (string, error) {
	binPath := filepath.Join(outDir, "relayly-server")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/relayly")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building server: %w", err)
	}
	return binPath, nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// RunningServer is the real relayly server started for the whole matrix run — every
// pair registers fresh devices against the same instance rather than paying a
// per-pair startup cost.
type RunningServer struct {
	cmd     *exec.Cmd
	BaseURL string
	WSURL   string
}

func startServer(binPath, dbDir string) (*RunningServer, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dbDir, "relayly.db")

	cmd := exec.Command(binPath, "start", "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--db.path", dbPath)
	cmd.Stderr = os.Stderr
	// Every shim connects from 127.0.0.1, so a thorough interop matrix trips the
	// default 10/minute per-IP WS upgrade limit almost immediately — raise it for
	// this harness-owned server instance only (internal/relay/ratelimit.go).
	cmd.Env = append(os.Environ(), "RELAYLY_WS_RATE_LIMIT_MAX=1000", "RELAYLY_WS_RATE_LIMIT_WINDOW_SECONDS=60")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting server: %w", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	rs := &RunningServer{cmd: cmd, BaseURL: baseURL, WSURL: fmt.Sprintf("ws://127.0.0.1:%d/ws", port)}

	if err := waitForHealth(baseURL, 10*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	return rs, nil
}

func waitForHealth(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server did not become healthy within %s", timeout)
}

func (rs *RunningServer) Stop() {
	if rs.cmd.Process != nil {
		_ = rs.cmd.Process.Kill()
	}
	_ = rs.cmd.Wait()
}

type deviceCreds struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

func registerDevice(baseURL, name string) (deviceCreds, error) {
	body := fmt.Sprintf(`{"name":%q}`, name)
	resp, err := http.Post(baseURL+"/api/v1/devices", "application/json", strings.NewReader(body))
	if err != nil {
		return deviceCreds{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return deviceCreds{}, fmt.Errorf("registering device %s: status %d", name, resp.StatusCode)
	}
	var creds deviceCreds
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return deviceCreds{}, err
	}
	return creds, nil
}
