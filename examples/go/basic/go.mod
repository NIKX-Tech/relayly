module github.com/NIKX-Tech/relayly/examples/go/basic

go 1.24

require github.com/NIKX-Tech/relayly/sdk/go v0.0.0

require (
	github.com/gorilla/websocket v1.5.1 // indirect
	golang.org/x/crypto v0.22.0 // indirect
	golang.org/x/net v0.21.0 // indirect
	golang.org/x/sys v0.19.0 // indirect
)

replace github.com/NIKX-Tech/relayly/sdk/go => ../../../sdk/go
