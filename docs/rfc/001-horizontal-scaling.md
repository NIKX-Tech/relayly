# RFC-001: Horizontal Scaling

- Status: **Proposed**
- Date: 2026-09-02
- Deciders: Relayly maintainers
- Affects: server (`internal/relay`, `internal/database`), deployment (`docker-compose.yml`), no SDK or wire-protocol change

## Problem

Relayly's server holds every piece of routing-relevant state in one process's memory,
with no external store and no cross-process coordination of any kind. Concretely, per
connected device the server holds two goroutines (a blocking read loop and a blocking
write loop, `internal/relay/client.go:114-124`) and one entry in the `Hub`'s connection
map:

```go
// internal/relay/hub.go:24-37
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client // deviceID → *Client
	...
	pairCodes *pairCodeRegistry
	...
}
```

Routing a message is a direct in-process map lookup and channel send:

```go
// internal/relay/hub.go:113-130
func (h *Hub) Route(msg Message, pairedDeviceID string) {
	h.mu.RLock()
	peer, ok := h.clients[pairedDeviceID]
	h.mu.RUnlock()

	if !ok {
		// Peer is offline - silently drop (client should handle reconnect)
		return
	}
	if !peer.enqueue(wsFrame{kind: websocket.BinaryMessage, data: msg.Payload}) {
		...
	}
}
```

If `pairedDeviceID` is not a key in *this process's* `clients` map, `Route` treats it as
offline and silently drops the message - there is no way to ask "is this device connected
to a different relay instance" because no such concept exists anywhere in the code.

Two more pieces of state have exactly the same shape:

- **Pairing codes** (`internal/relay/paircodes.go:20-27`): a `pair_request` on one
  instance stores a code in that instance's own `pairCodeRegistry`. The matching
  `pair_accept`, if it lands on a different instance, finds nothing (`Take` returns
  `ok: false`) and the pairing fails with `invalid_code` even though the code is real and
  unexpired.
- **Per-IP rate limiting** (`internal/relay/ratelimit.go:48-53`): `IPRateLimiter.buckets`
  is a `sync.Map` local to the process. Under N instances behind a load balancer with no
  shared state, a client's effective limit becomes `N ×` the configured one, silently.

Device/pairing *persistence* (who is paired with whom, device tokens, static keys) is
already externalized correctly - SQLite via `internal/database` (`migrations/001_init.sql`)
is a real durable store, not in-memory. The problem is entirely the *live connection and
routing* state layered on top of it, which has no persistence and no cross-instance
visibility at all.

**Consequence**: relayly cannot run more than one instance serving one logical mesh of
devices today. `docker-compose.yml` deploys exactly one container; there is no k8s
manifest, no `replicas: N` anywhere in this repo, and grepping the codebase and every doc
(`README.md`, `docs/PROTOCOL.md`, `docs/ROADMAP.md`, `docs/tasks/`) turns up zero mention
of horizontal scaling, connection-count limits, or per-connection resource budgets. This
is not a deliberately deferred, documented limitation - it is simply unaddressed. At
current usage this costs nothing; it becomes a real ceiling the moment device count
requires more than one relay process's worth of memory and file descriptors, or the
moment availability requirements need more than one process at all.

## Related work

Relayly's shape - a small, dumb, disposable, zero-knowledge WebSocket relay whose only
job is rendezvous and forwarding between two peers who do their own end-to-end crypto -
has one well-known production analogue: **Tailscale's DERP relays**
(Designated Encrypted Relay for Packets). DERP servers are independent, unlinked processes;
a client tries each one in its configured region list and picks the first that both peers
in a connection can reach, falling back to relaying through it only when a direct
peer-to-peer path isn't available. Critically, DERP does not solve "N replicas serving one
shared connection pool" either - it sidesteps the problem by making each relay instance
fully independent and letting *clients* (not the server) handle discovery and failover
across instances. Relayly already has a version of this in its own roadmap
(`docs/ROADMAP.md`: "Relay federation / multi-relay failover - SDKs accept a relay list;
sessions survive relay death") but that is a different property from this RFC's problem:
federation gives you failover between *wholly independent* relays (each with their own
device population), not horizontal scaling of *one* relay's capacity for *one shared* set
of paired devices, which is what's needed to serve a device population larger than one
process can hold.

The more directly applicable prior art is the standard pattern for scaling any stateful
WebSocket fan-out service: externalize the "who is connected to which process" mapping
and add a message bus so a miss on the local process can be handed to whichever process
actually holds the target connection. This is the same shape Slack, Discord, and most
chat/presence systems use at scale (a shared routing table plus Redis Pub/Sub, NATS, or an
equivalent) and is not a relayly-specific invention - the contribution here is applying it
correctly to a *zero-knowledge* relay, where the shared bus must carry only opaque
ciphertext and device IDs, never anything the relay could use to reconstruct who is
talking to whom beyond what it already necessarily knows today.

A more principled prior art for the presence half specifically is Phoenix's distributed
Presence (Elixir/Erlang, Chris McCord/José Valim): a CRDT-based design that tracks node
membership without a single external store or a single point of truth, converging
correctly under network partitions without an arbitrary staleness bound. It is the more
academically rigorous answer to "track presence across nodes" and is worth naming
precisely so the choice below reads as deliberate, not as an unresearched gap. This RFC
does not adopt it: a CRDT-gossip presence layer is a materially larger and more complex
piece of machinery than this project's stated design values support (`README.md`: "Small,
dependency-light codebase" and auditability as an explicit goal) - the two-line `Hub`
struct this RFC is replacing is itself evidence of how deliberately small the surface area
has stayed. A single external store (Redis) with an explicit, quantified staleness bound
(see Consistency and failure modes below) is a smaller, more auditable piece of machinery
that fits this project's actual constraints, at the cost of a real, bounded, and honestly
documented weaker guarantee than Phoenix Presence provides - not a guarantee this RFC
claims to match.

## Decision

Externalize the three pieces of process-local state identified above, behind a new
interface boundary, implemented incrementally rather than as one large change.

### 1. `Router` interface, replacing `Hub`'s direct map access

Introduce an interface capturing exactly what `Hub.Route`, `Hub.GetClient`, and the
Register/Unregister paths need:

```go
type Router interface {
	// RegisterLocal marks deviceID as connected to this process, replacing any
	// previous local connection for that device.
	RegisterLocal(deviceID string, client *Client)
	// UnregisterLocal removes deviceID's local connection, if it is still the
	// current one for that device (mirrors Hub.Unregister's wasCurrent check).
	UnregisterLocal(deviceID string, client *Client)
	// Route delivers payload to pairedDeviceID, wherever it is currently
	// connected - locally or on a different process.
	Route(payload []byte, fromDeviceID, pairedDeviceID string)
}
```

An in-memory implementation wrapping today's exact `Hub` behavior is the first (and
initially only) implementation - this step is a pure refactor with no behavior change,
landed on its own so every later step is additive rather than a rewrite.

### 2. Externalized routing table + pub/sub, behind the same interface

A second `Router` implementation backed by Redis (chosen over NATS for operational
simplicity - a single well-understood dependency likely already present in a production
deployment, versus introducing a new message-bus technology; revisit if a concrete need
for NATS's stronger delivery guarantees emerges):

- **Presence**: on `RegisterLocal`, `SET device:{id}:instance {this-instance-id} EX <ttl>`,
  refreshed on a heartbeat; on `UnregisterLocal`, `DEL`. This replaces the local `clients`
  map as the source of truth for "which process, if any, currently holds this device's
  connection."
- **Delivery**: `Route` first checks Redis for the peer's current instance. If it's this
  process, deliver locally exactly as `Hub.Route` does today. If it's a different
  instance (or unknown), publish the opaque payload to a Redis Pub/Sub channel keyed by
  the target device ID; every instance subscribes to channels for the devices it
  currently holds locally, and on receipt delivers to that device's local WebSocket
  exactly as if the message had arrived locally. The relay still never decrypts or
  inspects `payload` - Redis carries the same opaque bytes `Hub.Route` already forwards
  verbatim today, plus the device IDs it already necessarily handles.
- **Failure mode if Redis is unreachable**: fail closed for cross-instance delivery
  (same result as today's "peer offline, drop" for a peer on a different instance),
  never silently fall back to only-local delivery, which would look like intermittent,
  unexplainable message loss depending on which instance a client happened to land on.

### Consistency and failure modes (the part a TTL-based design must be honest about)

A `SET ... EX <ttl>` presence key is a real, known trade-off, not a solved problem, and
this RFC states its consequence precisely rather than leaving it implicit. If an instance
holding a device's connection dies ungracefully (process kill, node failure - anything
that skips `UnregisterLocal`'s `DEL`), the presence key for every device it held stays
correct-looking in Redis until its TTL expires. During that window, any other instance's
`Route` call for a message addressed to one of those devices believes the device is still
reachable, publishes to a Pub/Sub channel with no subscriber, and the message is silently
lost - not misdelivered, not retried, just dropped, exactly like a message to a genuinely
offline device today, except the sender has no way to tell the two cases apart until the
TTL lapses and the key naturally disappears.

This is a bounded-staleness guarantee, not linearizable presence, and the bound is exactly
the TTL/heartbeat-interval choice: a 5s TTL with a 2s heartbeat bounds the false-online
window to at most 5s after a hard crash (worst case: the instance dies immediately after a
successful heartbeat), at the cost of a heartbeat write every 2s per connected device. This
number needs to be chosen deliberately against relayly's actual connection volume and
Redis's own write capacity, not left as a placeholder - Phase B's implementation should
measure Redis's actual sustained SET rate under a realistic connection count before fixing
these constants, and this RFC's Evaluation section below requires that measurement, not
just a pass/fail functional check.

A graceful shutdown (SIGTERM, the normal rolling-deploy path) does not have this problem at
all - `UnregisterLocal`'s `DEL` removes the presence key immediately, the same instant
today's in-memory `Hub.Unregister` does. The staleness window is specifically, and only, a
hard-crash property. The Evaluation section below tests both cases separately for exactly
this reason: they are not the same test.

### 3. Pairing codes, same mechanism

Move `pairCodeRegistry`'s `Put`/`Take` to Redis (`SET pair:{code} {requesterID} EX <ttl>`,
`GETDEL` for atomic single-use consumption), so a `pair_accept` landing on a different
instance than the matching `pair_request` still succeeds. Same shared store as step 2,
not a second dependency.

### 4. Rate limiting: shared, with an explicit accepted trade-off

Move the token-bucket state to Redis as well, as a fixed-window limiter with one round
trip per request. `INCR` then a separate `PEXPIRE` is the wrong implementation to actually
ship: those are two round trips, and a crash or timeout between them leaves a counter key
with no expiry at all - a client's limit silently and permanently stuck at whatever count
it reached, never resetting. The correct version is Redis's own documented rate-limiting
recipe, one atomic Lua script via `EVAL` so the increment and the conditional expiry are a
single indivisible operation with no crash-between-two-commands gap:

```lua
local current = redis.call("INCR", KEYS[1])
if tonumber(current) == 1 then
    redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return current
```

Accept that this adds a Redis round trip to every WebSocket upgrade attempt specifically -
the hot path (message routing after a connection is established) is unaffected. If this
measurably matters in practice, a documented fallback is a local-approximate limiter (each
instance enforces `configured-limit / instance-count`) traded explicitly against
precision, not a silent per-instance limit as today.

### 5. SQLite → real multi-writer store, decided by what's actually shared

Device/pairing persistence (`internal/database`) is not touched by steps 1-4 above - it
already sits behind a clean `*DB` boundary and is read relatively rarely compared to the
live routing path (device lookup at connect time, pairing state changes, not per-message).
Two options, not decided in this RFC: (a) move it to a real multi-writer database
(Postgres) once multi-instance deployment is real, since N processes cannot safely share
one SQLite file over a network volume; or (b) keep SQLite as a per-instance read replica
for data that changes rarely (device records) while routing-hot-path state lives
exclusively in Redis per steps 1-4. Decide this once step 2 is implemented and its actual
read/write pattern against `internal/database` is measured, rather than guessing now.

## Rejected alternatives

- **Sticky routing at the load balancer (consistent hashing on device ID, no shared
  state).** Rejected as insufficient on its own: it solves "the same device always lands
  on the same instance," which helps but does not solve the actual problem - two
  *different, paired* devices could still deterministically hash to *different*
  instances, and there is still no cross-instance bus to route between them. Sticky
  routing could be a useful complementary optimization once step 2 exists (reduces
  cross-instance Pub/Sub traffic by making the common case land locally), but is not a
  substitute for it.
- **NATS instead of Redis for the pub/sub layer.** Not rejected outright - noted as the
  fallback if Redis's guarantees prove insufficient - but Redis is chosen first for
  simplicity and because presence/TTL keys and pub/sub both live naturally in one
  well-understood dependency, rather than introducing a second piece of infrastructure
  before there's a concrete reason to.
- **Federation (`docs/ROADMAP.md`'s existing "relay federation / multi-relay failover"
  item) as a substitute for this work.** Rejected as solving a different problem -
  federation is about surviving the death of one *independent* relay by falling back to
  another with its own separate device population; it does not let one logical mesh of
  paired devices exceed one process's connection capacity, which is this RFC's actual
  problem.

## Consequences

- No SDK or wire-protocol change. Every device-facing behavior (`docs/PROTOCOL.md`) is
  unaffected - this is entirely a server-internal architecture change, observable only as
  "the server now works correctly when deployed as more than one instance."
- New runtime dependency: Redis, required only for multi-instance deployment. A
  single-instance deployment (today's default, and likely most self-hosted deployments)
  keeps using the in-memory `Router` implementation from step 1 with zero new
  dependencies - Redis is opt-in, gated by config, not a requirement imposed on every
  deployment.
- **A single Redis instance is a new single point of failure, and this RFC does not
  pretend otherwise.** Today, one relay process is the whole system's SPOF. After this
  RFC, N relay processes plus one Redis instance replaces it - strictly better for
  *capacity* (more devices than one process can hold), but not automatically better for
  *availability* unless Redis itself is deployed HA (Sentinel or Cluster). Until it is,
  killing Redis degrades every relay instance to routing only its own locally-connected
  devices (a real, survivable degradation - not a total outage, since local delivery never
  depended on Redis) but breaks all cross-instance delivery and new pairings at once. A
  production multi-instance deployment needs HA Redis to actually deliver on the
  availability half of "horizontal scaling," not just the capacity half; this RFC scopes
  that as a deployment requirement to document, not as something Phase B's code needs to
  solve itself.
- `internal/relay/hub.go` loses direct ownership of routing; `Client` objects stay
  process-local (a live goroutine pair and channel can't meaningfully live anywhere else),
  but "is device X reachable, and where" becomes a question the `Router` interface
  answers, not a question answered by reading a local map.
- CI gains a multi-instance integration test (see Evaluation below) - the current test
  suite has no coverage of this scenario at all, since it's never been possible to
  exercise.

## Evaluation

Two relay instances, run behind a load balancer configured with **no** sticky/session
affinity (deliberately, to force devices onto different instances rather than let a
lucky routing outcome hide a real bug), against a single-node Redis for the base case:

**Functional correctness**

1. Two devices connect; confirm (via the admin UI or a debug endpoint, not by inference)
   which instance each actually landed on, and that they differ.
2. A `pair_request` issued against one instance and the matching `pair_accept` issued
   against the other: confirm pairing succeeds.
3. Once paired, confirm a binary envelope sent by one device is delivered to the other,
   round-trip, regardless of which instance either is connected to.
4. **Graceful shutdown** of one instance (SIGTERM): confirm the still-connected device's
   peer is correctly and immediately reported offline (`notifyPeerStatus`), and a
   reconnect from the terminated instance's former device lands on the surviving instance
   with pairing state intact.
5. **Hard crash** of one instance (SIGKILL), tested separately from step 4 because the two
   have genuinely different failure semantics per the Consistency and failure modes
   section above: confirm messages addressed to the crashed instance's former devices are
   dropped (not misdelivered) for up to the configured TTL, and confirm the surviving
   instance's own devices are entirely unaffected throughout.
6. Confirm rate limiting behaves per whichever of step 4 (Decision, not Evaluation)'s two
   documented options was implemented - shared count via the atomic Lua script, or an
   explicitly-accepted per-instance approximation - not silently multiplied by instance
   count, and confirm the Lua script's atomicity specifically: no key is ever left without
   a TTL under concurrent load (a race the two-command `INCR`/`PEXPIRE` version could
   produce, which is exactly why it was rejected above).

**Measured, not just observed** - this is the part that makes the design's own stated
trade-offs verifiable rather than asserted:

7. The false-online staleness window from step 5, measured directly (time from SIGKILL to
   the last successful cross-instance delivery attempt against that instance's devices),
   compared against the configured TTL to confirm the bound actually holds in practice,
   not just in theory.
8. Added per-message latency from the Redis round trip (local-delivery case vs.
   cross-instance case), under a realistic connected-device count, to give the TTL/
   heartbeat-interval choice in the Consistency section a real number to be tuned against
   instead of a placeholder.
9. Redis killed outright: confirm each instance degrades to local-only delivery (per the
   Consequences section's documented degradation) rather than crashing or wedging, and
   recovers cross-instance delivery automatically once Redis returns, with no relay
   process restart required.

This becomes the interop-style regression test this class of bug needs, the same way
RFC-000's protocol drift led to a mandatory cross-language interop matrix in CI
(`docs/tasks/02-sdks-and-interop.md`) - a multi-instance scenario currently has zero
coverage anywhere in this repo, and should gain permanent CI coverage once implemented,
not just a one-time manual verification. Items 7-8's numbers belong in this RFC's
implementation PR description and in `docs/ROADMAP.md`'s own record of the work, not
just a passing CI job - they are the actual evidence behind every quantitative claim
this document makes.
