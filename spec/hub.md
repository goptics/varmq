# VarMQ hub protocol

**Version 1.0.0**

This document is the contract between a VarMQ process (the **instance**) and a hub such as vartop. The instance dials out to the hub and keeps one stream open. The hub sends REST requests of `spec/openapi.yaml` down that stream, and the instance runs each one on its own `varmq.Handler` and posts the reply. varmq's `ConnectHub` is the Go implementation of the instance side. A change to anything below starts here, with a version bump.

The words MUST, MUST NOT, SHOULD and MAY are used as in RFC 2119.

## Changes

- **1.0.0**: first version (protocol `1`). Stream, reply and beat endpoints; message types `hello`, `request` and `ping`. A session secret per stream, sent in `hello` and carried by every reply and beat in `X-Hub-Session`.

## Terms

- **Instance**: one VarMQ process. Its **Instance ID** is `varmq.InstanceID`: 1 to 128 bytes of valid UTF-8 with no control character (no byte below `0x20`, no `0x7F`), unique among the instances of one hub. An instance MUST refuse to start with another ID.
- **Hub**: the server the instances dial, for example vartop.
- **Token**: a shared secret. Every instance request carries `Authorization: Bearer <token>`. The token MUST NOT be sent in a URL.
- **Session**: a secret the hub creates for each stream and sends in `hello`. Every reply and beat of that stream carries it in the header `X-Hub-Session`. The session MUST NOT be sent in a URL or written to a log.
- `{hub}`: the hub base URL, for example `https://vartop.internal`. It MAY have a path; the endpoints below are appended to it.

The transport is plain HTTP/1.1 or HTTP/2. No WebSocket or other upgrade is used.

## 1. Stream (hub → instance)

```
GET {hub}/hub/stream?instance=<instance id>&protocol=1
```

- `instance`: the Instance ID, query-escaped.
- `protocol`: the protocol version the instance speaks, `1`.

Hub replies:

| Status | Meaning |
|---|---|
| `200` | `Content-Type: application/x-ndjson`; the body stays open. |
| `400` | `instance` missing, over 128 bytes, not valid UTF-8 or with a control character; `protocol` not `1`; or the stream request carries a body. |
| `401` | Token missing or wrong. |
| `409` | An instance with that ID is already connected. The hub never replaces a live stream on a new connect. |
| `503` | The hub is at its instance limit, its new-stream rate limit, or is closing. |

The body is one JSON object per line, ended by `\n`. The hub flushes after each line. A line is at most 64 KiB.

### 1.1 Messages

| Type | Fields | Meaning |
|---|---|---|
| `hello` | `protocol`, `heartbeat_ms`, `session` | Always the first line. `heartbeat_ms` is 1000 to 60000. |
| `request` | `id`, `method`, `path` | One REST request to run. |
| `ping` | none | Sent by the hub every `heartbeat_ms`. |

```json
{"type":"hello","protocol":1,"heartbeat_ms":5000,"session":"q3Vh0Zk-8wR2_tLmP6yXcA"}
{"type":"request","id":"r-81","method":"GET","path":"/workers/emails?x=1"}
{"type":"ping"}
```

- `session` is 22 to 64 bytes of `[A-Za-z0-9_-]`, with at least 128 random bits, new for every stream.
- `id` is opaque: 1 to 64 bytes of `[A-Za-z0-9_-]`.
- `method` is `GET` or `PATCH`.
- `path` starts with `/` and is a path plus an optional query, relative to the handler root (no prefix). The API has no request bodies.

A receiver MUST ignore a line whose `type` it does not know, and MUST ignore fields it does not know.

An instance MUST clamp a `heartbeat_ms` outside 1000 to 60000 into that range. It MUST treat the stream as dead and redial when it has received no line for 3 × `heartbeat_ms`.

A `hello` without a valid `session` ends the stream: the instance redials. An instance MUST drop a `request` that arrives before `hello`: it has no session to reply with.

## 2. Reply (instance → hub)

```
POST {hub}/hub/reply/<id>?instance=<instance id>
```

Headers:

- `X-Hub-Session`: the session of the stream the request came on.
- `X-Hub-Status`: the status code of the API reply.
- `Content-Type`: the API reply's, when it has one.
- `X-Varmq-Instance`: the API reply's.

The body is the API reply body, raw, at most 16 MiB.

Hub replies:

| Status | Meaning |
|---|---|
| `204` | Delivered. |
| `400` | `X-Hub-Status` missing or not a status code from 200 to 599, or the hub could not read the reply body (for example its 10 s read limit cut a slow body). |
| `404` | No request with that ID is waiting from that instance (expired or unknown), or the instance has no stream with that session (header missing, wrong value, or an old stream). |
| `401` | Token missing or wrong. |
| `413` | Body over 16 MiB. |
| `503` | The hub's reply-memory budget (per server) is full. |

An instance MUST answer a request it cannot run (method not `GET` or `PATCH`, or `path` not starting with `/`) with `X-Hub-Status: 400` and a JSON body `{"error":"..."}`, when the frame has a valid `id`. A frame without a valid `id` is dropped.

An instance MUST NOT post a reply outside a live session: a reply whose stream ended is dropped.

## 3. Beat (instance → hub)

```
POST {hub}/hub/beat?instance=<instance id>
POST {hub}/hub/beat?instance=<instance id>&bye=1
```

Every beat, `bye=1` included, carries the header `X-Hub-Session` with the session of its stream.

The instance posts its first beat at once after it reads `hello`, then one every `heartbeat_ms`. It MUST NOT post a beat outside a live session: none before `hello`, none after the stream ended. With `bye=1` the instance is stopping, or its stream ended, and the hub removes it at once.

Hub replies:

| Status | Meaning |
|---|---|
| `204` | Recorded. |
| `404` | That instance has no stream with that session (header missing, wrong value, or an old stream). The instance redials. |
| `401` | Token missing or wrong. |

The hub drops an instance (closes its stream and fails its waiting requests) when it has had no beat for 3 × `heartbeat_ms`. The opening of the stream starts this clock. The hub lists an instance as connected (for discovery and requests) only after the first beat of its session; the instance sends that beat at once, so this costs no delay.

When a stream that received `hello` ends for any reason while the instance keeps running, the instance SHOULD post `bye=1` with that stream's session before it redials (best effort, 2 s limit). The hub then frees the ID at once, and the redial does not meet a `409`. The `bye` on stop also carries the session. No `bye` is sent for a stream without `hello`.

## 4. Rules

### 4.1 Hub

- The hub MUST NOT queue a request for an instance that is not connected: the caller gets an error at once.
- The hub fails a waiting request when its caller's context ends or the instance's stream closes.
- The hub MUST compare tokens and sessions in constant time.
- The hub MUST NOT replace a live stream on a new connect: a duplicate ID gets `409`.

### 4.2 Instance

- At most 8 requests run at once. Further frames wait in order.
- Reconnect with exponential backoff and jitter. The first retry comes within 0.5 to 1 s. The delay `d` doubles to a cap of 30 s, and each sleep is random in [`d`/2, `d`]. The delay resets after a stream that received `hello`.
- A `401` or `400` from the stream goes to an optional error callback, and the instance keeps retrying at the cap delay. A rotated token then heals without a restart.
- Any other status from the stream (`409`, `503` and others) goes to the callback, and the instance retries with the normal backoff.
- Before it redials after a stream that received `hello`, the instance posts `bye=1` with that stream's session (section 3).
- The instance MUST NOT connect over `http://` to a host that is not loopback, unless an explicit option allows it: the token would travel in clear text.
- The hub closes idle connections after 30 s. The instance's HTTP client SHOULD keep idle connections for less (varmq: 20 s), or a reply POST can land on a connection the hub just closed and be lost.
- Nothing in the instance's hub code may run on, or share a lock with, the job path (add, dispatch, ack).

### 4.3 Instance behaviour in varmq (informative)

These are choices of `ConnectHub`, not requirements on a hub.

- Trust: the token proves the instance to the hub; only TLS and the hub URL prove the hub. Whoever answers at the hub URL gets everything `varmq.Handler` allows: by default it reads job payloads and results, stops and resizes workers, pauses queues, and cancels jobs and workflows. On shared storage a queue pause or a job cancel reaches every consumer process. `WithHubInsecure` lets a party on the network path act as the hub, not only read the token.
- A hub URL with userinfo is refused. Configuration errors do not repeat the URL.
- No request follows a redirect: a `3xx` is an unexpected status, and the token goes to the hub URL only.
- `heartbeat_ms` absent or `0` in `hello` means the instance's configured heartbeat (default 5000). Any other value is clamped to 1000 to 60000. A second `hello` on the same stream is ignored.
- A stream line over 64 KiB ends the stream; the instance redials. So does a first `hello` whose `protocol` is not `1`, whose `session` is missing or invalid, or whose fields have the wrong JSON type. The session error has a fixed text and never repeats the value.
- A line with no `type`, or a `type` other than `hello`, `request` or `ping`, is ignored whole, whatever its fields hold. A line that is not a JSON object (an empty line included), a line whose `type` is not a string, a second `hello` or a `request` with fields of the wrong JSON type, a `request` without a valid `id`, or a `request` before `hello` is a bad frame: it is reported (hub text cut to 80 bytes) and dropped. 16 bad frames end the stream, so the redial backoff bounds the rate.
- At most 8 requests run at once, across redials. Up to 64 more frames wait in order. When the queue is full the instance stops reading the stream; if it stays full for 3 × `heartbeat_ms`, the instance ends the stream and redials.
- A `request` runs on a request context that ends when the stream drops. A request that is still running when its stream ends keeps its slot until its handler returns; its reply is not sent.
- A reply is posted once, with a 30 s limit. The hub reads a reply body for at most 10 s, so a slower reply shows on the instance as a reply `400`. No reply is retried.
- An API reply over 16 MiB is answered with `X-Hub-Status: 502` and `{"error":"reply over 16 MiB"}`. A handler panic is answered with `500` and reported. Reply-failure and panic reports are rate-limited per stream: the first goes at once, then at most one per heartbeat interval, ending "(and N more)" for the suppressed ones.
- A beat answered `401` ends the stream and retries at the cap delay, as a stream `401` does. A `409` or `503` from the stream retries with the normal backoff.
- The `bye` after a stream ends, and the `bye` on a clean stop, go once per stream that received `hello`, each on its own context with a 2 s limit. A stop during the backoff sleep sends no second `bye`. On a half-open HTTP/2 connection the `bye` can use its 2 s and be lost; the redial then dials fresh.
- `InstanceID` is checked when `ConnectHub` starts. A bad ID returns a fixed error that does not repeat it.
- Read-only mode answers every `PATCH` with `403` and `{"error":"read-only hub connection"}`, without running the handler.
- No-payload mode answers `GET /queues/{id}/jobs` and `GET /queues/{id}/jobs/{jobId}`, in any path spelling, with `payload` set to `null` and `result` removed (`result_size` stays). Job error texts (`errors[].error`) and workflow task `reason` texts are still sent; they can hold application data.
- The hub connection holds no job-path lock. It runs the same handlers as a mounted `varmq.Handler`, which turns on the same job tracking as `varmq.NewServerMux`.

### 4.4 Token scope

One token per server means an instance of that server can connect under any unused ID of that server, or hold an ID before its owner connects. The session secret stops it from evicting, or answering for, an instance that is connected.

## 5. Compatibility

- The `protocol` query parameter and the `hello` field carry the version. A later version adds message types, which an older receiver ignores (section 1.1).
- A new message type or field is a minor version. A changed meaning, or a removed or renamed endpoint, header, field or status, is a major version and a new `protocol` number.
