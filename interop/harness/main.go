// Command harness drives the cross-language interop matrix (docs/tasks/
// 02-sdks-and-interop.md): it builds/starts the real relayly server, launches each
// SDK's CLI shim (interop/clients/<lang>/) through an in-process WebSocket proxy,
// and runs the positive pairing/roundtrip/reconnect flow across all 10 SDK pairs plus
// three negative scenarios per SDK.
//
// interop/harness is its own Go module (like sdk/go), so run it from within this
// directory rather than `go run ./interop/harness` from the repo root (that only
// works with a local go.work file, which is gitignored):
//
//	cd interop/harness && go run .
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "interop harness:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	tmpRoot, err := os.MkdirTemp("", "relayly-interop-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpRoot)

	buildDir := tmpRoot + "/build"
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return err
	}

	fmt.Println("Building server...")
	serverBin, err := buildServerBinary(root, buildDir)
	if err != nil {
		return err
	}

	fmt.Println("Preparing SDK shims...")
	sdks, err := prepareSDKs(root, buildDir)
	if err != nil {
		return err
	}

	fmt.Println("Starting server...")
	dbDir := tmpRoot + "/db"
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return err
	}
	server, err := startServer(serverBin, dbDir)
	if err != nil {
		return err
	}
	defer server.Stop()

	proxy, err := NewProxy(server.WSURL)
	if err != nil {
		return err
	}
	defer proxy.Close()

	fmt.Println("Running interop matrix...")
	pairResults, negResults, allOK := runMatrix(server, proxy, sdks, tmpRoot)
	printSummary(pairResults, negResults)

	if !allOK {
		return fmt.Errorf("one or more interop scenarios failed")
	}
	fmt.Println("All interop scenarios passed.")
	return nil
}
