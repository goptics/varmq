# `Reject` joins `IAcknowledgeable`, in v1

`IAcknowledgeable` gains `Reject(ackID string, reason string) bool` beside `Acknowledge`: `Acknowledge` means completed, `Reject` means failed for the last time with no retry. A failure with retries left calls neither. Storage needs the outcome to keep Job Records and counters (see `spec/redis-storage.md`), and only the worker knows it. The method is required, not optional, so every acknowledgeable storage reports outcomes the same way. This breaks custom `IAcknowledgeable` implementations; it ships in a v1 minor release, flagged in the changelog, because few users implement their own storage.

## Considered Options

- **An optional interface**: rejected; outcome reporting would depend on each backend opting in.
- **`Fail` / `Nack`**: rejected; `Reject` is the AMQP term for "remove without requeue", and redisq already uses "nacked" for not-yet-acknowledged jobs.
- **`reason error`**: rejected; the spec stores the error text, and a Go `error` does not cross languages.
- **varmq v2 module path**: rejected; the user base is small enough that a major version adds cost without protecting anyone.
