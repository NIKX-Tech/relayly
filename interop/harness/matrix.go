package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// prepareSDKs builds the compiled shims (Go, Rust) and confirms the interpreted ones
// (TS, Python) are ready to run. TS/Python dependency installation
// (`npm install` in interop/clients/ts, `pip install -e sdk/py`) is a documented
// prerequisite run once before the harness, not repeated here, matching how CI
// already separates "install deps" from "run tests" for every other SDK.
func prepareSDKs(root, buildDir string) (map[string]SDKDef, error) {
	defs := make(map[string]SDKDef)

	// interop/clients/go has its own go.mod (a separate module, like sdk/go), so the
	// build must run with that directory as the module context — "go build
	// ./interop/clients/go" from the repo root would fail with "main module does not
	// contain package", the same reason this whole repo's CI already `cd`s into
	// sdk/go rather than relying on a workspace file (go.work is gitignored, a local
	// dev convenience, not something CI or a fresh clone can rely on).
	goBin := filepath.Join(buildDir, "go-shim")
	goShimDir := filepath.Join(root, "interop", "clients", "go")
	if err := runIn(goShimDir, "go", "build", "-o", goBin, "."); err != nil {
		return nil, fmt.Errorf("building go shim: %w", err)
	}
	defs["go"] = SDKDef{Name: "go", Command: goBin}

	rustDir := filepath.Join(root, "interop", "clients", "rust")
	if err := runIn(rustDir, "cargo", "build"); err != nil {
		return nil, fmt.Errorf("building rust shim: %w", err)
	}
	defs["rust"] = SDKDef{Name: "rust", Command: filepath.Join(rustDir, "target", "debug", "interop-shim-rust")}

	tsDir := filepath.Join(root, "interop", "clients", "ts")
	tsxBin := filepath.Join(tsDir, "node_modules", ".bin", "tsx")
	if _, err := os.Stat(tsxBin); err != nil {
		return nil, fmt.Errorf("ts shim not ready: %s not found — run `npm install` in %s first", tsxBin, tsDir)
	}
	defs["ts"] = SDKDef{Name: "ts", Command: tsxBin, Args: []string{filepath.Join(tsDir, "shim.ts")}}

	pyShim := filepath.Join(root, "interop", "clients", "py", "shim.py")
	if err := exec.Command("python3", "-c", "import relayly").Run(); err != nil {
		return nil, fmt.Errorf("py shim not ready: `import relayly` failed — run `pip install -e sdk/py` first")
	}
	defs["py"] = SDKDef{Name: "py", Command: "python3", Args: []string{pyShim}}

	return defs, nil
}

func runIn(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stderr
	return cmd.Run()
}

type pairResult struct {
	a, b   string
	pass   bool
	errMsg string
}

type negativeResult struct {
	scenario string
	sdk      string
	pass     bool
	errMsg   string
}

// runMatrix runs the positive flow across all 10 SDK pairs and the three negative
// scenarios once per SDK (each in the "victim" role, paired against go), returning
// results for the summary table.
func runMatrix(server *RunningServer, proxy *Proxy, sdks map[string]SDKDef, tmpRoot string) ([]pairResult, []negativeResult, bool) {
	names := []string{"go", "ts", "py", "rust"}
	var pairs [][2]string
	for i, a := range names {
		for _, b := range names[i:] {
			pairs = append(pairs, [2]string{a, b})
		}
	}

	allOK := true
	var pairResults []pairResult
	for _, pair := range pairs {
		aDef, bDef := sdks[pair[0]], sdks[pair[1]]
		workDir, err := os.MkdirTemp(tmpRoot, "pair-*")
		if err != nil {
			pairResults = append(pairResults, pairResult{a: pair[0], b: pair[1], pass: false, errMsg: err.Error()})
			allOK = false
			continue
		}
		err = runPositiveFlow(server, proxy, aDef, bDef, workDir)
		pairResults = append(pairResults, pairResult{a: pair[0], b: pair[1], pass: err == nil, errMsg: errString(err)})
		if err != nil {
			allOK = false
		}
	}

	var negResults []negativeResult
	negScenarios := []struct {
		name string
		fn   func(*RunningServer, *Proxy, SDKDef, SDKDef, string) error
	}{
		{"wrong_pin", runWrongPinTest},
		{"key_rewrite", runKeyRewriteTest},
		{"rekey_safety", runRekeySafetyTest},
	}
	for _, sdkName := range names {
		for _, sc := range negScenarios {
			workDir, err := os.MkdirTemp(tmpRoot, "neg-*")
			if err != nil {
				negResults = append(negResults, negativeResult{scenario: sc.name, sdk: sdkName, pass: false, errMsg: err.Error()})
				allOK = false
				continue
			}
			partner := "go"
			if sdkName == "go" {
				partner = "ts" // go can't be its own fixed partner
			}
			err = sc.fn(server, proxy, sdks[sdkName], sdks[partner], workDir)
			negResults = append(negResults, negativeResult{scenario: sc.name, sdk: sdkName, pass: err == nil, errMsg: errString(err)})
			if err != nil {
				allOK = false
			}
		}
	}

	return pairResults, negResults, allOK
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func printSummary(pairResults []pairResult, negResults []negativeResult) {
	fmt.Println()
	fmt.Println("=== Positive flow (register, pair, roundtrip, reconnect+rekey) ===")
	for _, r := range pairResults {
		status := "PASS"
		if !r.pass {
			status = "FAIL"
		}
		fmt.Printf("  %-4s x %-4s  %s\n", r.a, r.b, status)
		if !r.pass {
			fmt.Printf("           %s\n", r.errMsg)
		}
	}

	fmt.Println()
	fmt.Println("=== Negative scenarios (each SDK once, in the victim role) ===")
	for _, r := range negResults {
		status := "PASS"
		if !r.pass {
			status = "FAIL"
		}
		fmt.Printf("  %-14s %-4s  %s\n", r.scenario, r.sdk, status)
		if !r.pass {
			fmt.Printf("                       %s\n", r.errMsg)
		}
	}
	fmt.Println()
}
