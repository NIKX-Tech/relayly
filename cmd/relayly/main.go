// Relayly — lightweight, self-hosted WebSocket relay for local-first apps.
// Entry point: wires together the Cobra CLI.
package main

import "github.com/NIKX-Tech/relayly/internal/cli"

func main() {
	cli.Execute()
}
