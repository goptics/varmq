# VarMQ hub protocol

**Version 1.0.0**

This document is the contract between a VarMQ process (the **instance**) and a hub such as vartop. The instance dials out to the hub and keeps one stream open. The hub sends REST requests of `spec/openapi.yaml` down that stream, and the instance runs each one on its own `varmq.Handler` and posts the reply. varmq's `ConnectHub` is the Go implementation of the instance side. A change to anything below starts here, with a version bump.

The words MUST, MUST NOT, SHOULD and MAY are used as in RFC 2119.

## Changes

- **1.0.0**: first version (protocol `1`). Stream, reply and beat endpoints; message types `hello`, `request` and `ping`.

## Terms

- **Instance**: one VarMQ process. Its **Instance ID** is `varmq.InstanceID`: 1 to 128 bytes, unique among the instances of one hub.
- **Hub**: the server the instances dial, for example vartop.
- **Token**: a shared secret. Every instance request carries `Authorization: Bearer <token>`. The token MUST NOT be sent in a URL.
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
| `400` | `instance` missing or over 128 bytes, or `protocol` not `1`. |
| `401` | Token missing or wrong. |
| `409` | An instance with that ID is already connected. |

The body is one JSON object per line, ended by `\n`. The hub flushes after each line. A line is at most 64 KiB.

### 1.1 Messages

| Type | Fields | Meaning |
|---|---|---|
| `hello` | `protocol`, `heartbeat_ms` | Always the first line. `heartbeat_ms` is 1000 to 60000. |
| `request` | `id`, `method`, `path` | One REST request to run. |
| `ping` | none | Sent by the hub every `heartbeat_ms`. |

```json
{"type":"hello","protocol":1,"heartbeat_ms":5000}
{"type":"request","id":"r-81","method":"GET","path":"/workers/emails?x=1"}
{"type":"ping"}
```

- `id` is opaque: 1 to 64 bytes of `[A-Za-z0-9_-]`.
- `method` is `GET` or `PATCH`.
- `path` starts with `/` and is a path plus an optional query, relative to the handler root (no prefix). The API has no request bodies.

A receiver MUST ignore a line whose `type` it does not know, and MUST ignore fields it does not know.

An instance MUST clamp a `heartbeat_ms` outside 1000 to 60000 into that range. It MUST treat the stream as dead and redial when it has received no line for 3 × `heartbeat_ms`.

## 2. Reply (instance → hub)

```
POST {hub}/hub/reply/<id>?instance=<instance id>
```

Headers:

- `X-Hub-Status`: the status code of the API reply.
- `Content-Type`: the API reply's, when it has one.
- `X-Varmq-Instance`: the API reply's.

The body is the API reply body, raw, at most 16 MiB.

Hub replies:

| Status | Meaning |
|---|---|
| `204` | Delivered. |
| `404` | No request with that ID is waiting from that instance (expired or unknown). |
| `401` | Token missing or wrong. |
| `413` | Body over 16 MiB. |

An instance MUST answer a request it cannot run (method not `GET` or `PATCH`, or `path` not starting with `/`) with `X-Hub-Status: 400` and a JSON body `{"error":"..."}`, when the frame has a valid `id`. A frame without a valid `id` is dropped.

## 3. Beat (instance → hub)

```
POST {hub}/hub/beat?instance=<instance id>
POST {hub}/hub/beat?instance=<instance id>&bye=1
```

The instance posts a beat every `heartbeat_ms`. With `bye=1` the instance is stopping, and the hub removes it at once.

Hub replies:

| Status | Meaning |
|---|---|
| `204` | Recorded. |
| `404` | That instance has no open stream. The instance redials. |
| `401` | Token missing or wrong. |

The hub drops an instance (closes its stream and fails its waiting requests) when it has had no beat for 3 × `heartbeat_ms`. The opening of the stream counts as a beat.

## 4. Rules

### 4.1 Hub

- The hub MUST NOT queue a request for an instance that is not connected: the caller gets an error at once.
- The hub fails a waiting request when its caller's context ends or the instance's stream closes.
- The hub MUST compare tokens in constant time.

### 4.2 Instance

- At most 8 requests run at once. Further frames wait in order.
- Reconnect with exponential backoff and jitter. The first retry comes within 0.5 to 1 s. The delay `d` doubles to a cap of 30 s, and each sleep is random in [`d`/2, `d`]. The delay resets after a stream that received `hello`.
- A `401` or `400` from the stream goes to an optional error callback, and the instance keeps retrying at the cap delay. A rotated token then heals without a restart.
- The instance MUST NOT connect over `http://` to a host that is not loopback, unless an explicit option allows it: the token would travel in clear text.
- Nothing in the instance's hub code may run on, or share a lock with, the job path (add, dispatch, ack).

### 4.3 Instance behaviour in varmq (informative)

These are choices of `ConnectHub`, not requirements on a hub.

- A stream line over 64 KiB, or a `hello` whose `protocol` is not `1`, ends the stream; the instance redials.
- A `request` runs on a request context that ends when the stream drops.
- An API reply over 16 MiB is answered with `X-Hub-Status: 502` and `{"error":"reply over 16 MiB"}`. A handler panic is answered with `500`.
- A beat answered `401` ends the stream and retries at the cap delay, as a stream `401` does. A `409` from the stream retries with the normal backoff.
- On a clean stop, the instance posts `bye=1` once, with a short timeout, if it ever received `hello`.
- Read-only mode answers every `PATCH` with `403` and `{"error":"read-only hub connection"}`, without running the handler.
- No-payload mode answers `GET /queues/{id}/jobs` and `GET /queues/{id}/jobs/{jobId}` with `payload` set to `null` and `result` removed. A reply it cannot decode is answered with `502`, never forwarded.

## 5. Compatibility

- The `protocol` query parameter and the `hello` field carry the version. A later version adds message types, which an older receiver ignores (section 1.1).
- A new message type or field is a minor version. A changed meaning, or a removed or renamed endpoint, header, field or status, is a major version and a new `protocol` number.
