// Relayly — lightweight, self-hosted WebSocket relay for local-first apps.
// Entry point: wires together the Cobra CLI.
package main

import "github.com/nikx-one/relayly/internal/cli"

func main() {
	cli.Execute()
}
