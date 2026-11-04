# varmq

A typed worker pool and message queue, with pluggable storage and an HTTP API that observers such as vartop read.

## Language

**Spec**:
The versioned contract in `spec/` that every implementation and observer follows: the HTTP API and the Redis storage layout.
_Avoid_: protocol, schema, contract module

**Queue ID**:
The identity a storage reports for a queue, the same on every instance (for redisq, its full Redis name); observers group queue data by it.
_Avoid_: queue key, queue name

**Job State**:
Where a job is in its life: pending, processing, completed (acknowledged) or failed (rejected).
_Avoid_: status

**Reject**:
The final failure of a job: no retry follows, and it is not requeued. The failure counterpart of Acknowledge.
_Avoid_: fail, nack, dead

**Job Record**:
What storage keeps about a completed or failed job after it leaves the queue: its payload, times, attempt count and the errors of its failed attempts.
_Avoid_: job history, job log, result
