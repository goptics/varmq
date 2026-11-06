# varmq Redis storage

**Version 1.3.0**

This document is the contract for a varmq queue kept in Redis. redisq is its Go implementation. A TypeScript or Rust implementation that follows it can produce to, consume from and inspect the same queue as redisq, and `spec/openapi.yaml` describes how the result reaches observers. A change to anything below starts here, with a version bump.

The words MUST, MUST NOT, SHOULD and MAY are used as in RFC 2119.

## Changes

- **1.3.0**: queue pause (sections 6.7, 6.8): key `<base>:paused` and channel `<base>:control`; every pop checks the key. Nothing from 1.2.0 changes while the key is absent.
- **1.2.0**: a worker entry gains `completed`, `successful` and `failed`; an instance reports at once on a lifecycle change (section 8.3). Nothing from 1.1.0 changes.
- **1.1.0**: adds Instance Records (section 8), new keys only, and the reader rule for unknown Job States (section 7). Nothing from 1.0.0 changes.
- **1.0.0**: first version. Describes the key layout redisq already uses, and adds Job Records, retention, the `:stats` counters, the `:started` and `:attempts` hashes, the `enqueued_at` envelope field and the `Reject` operation. Purge deletes all job data, as redisq PR #28 does, plus the new keys.

## Terms

Terms:

- **Queue ID**: the identity a storage reports for a queue, the same on every instance. Here it is the queue's base key (below), for example `redisq:shop:fifo:{emails}`.
- **Job State**: `pending` (in the queue), `processing` (handed out to a consumer, no outcome yet), `completed` (acknowledged) or `failed` (rejected). Later versions can add states (section 7).
- **Reject**: the final failure of a job. No retry follows, and the job is not requeued.
- **Job Record**: what storage keeps about a completed or failed job after it leaves the queue.

Two more terms are used here:

- **Item**: the bytes stored in the queue for one job. Section 3 defines its format, the envelope.
- **Owner**: one queue object in one process, identified by a random string. Every item it hands out is pending under an ack ID that starts with the owner.
- **Instance**: one varmq process. It holds zero or more owners.
- **Instance Record**: what an instance writes about itself and its workers (section 8). It is not the owner heartbeat in `<base>:heartbeats`.

## 1. Keys

### 1.1 Base key

Every queue has one base key:

```
redisq:[<namespace>:]<kind>:{<queue key>}
```

- `<namespace>` is optional. When it is absent, the colon after it is absent too: `redisq:fifo:{emails}`. It MUST NOT contain `{` or `}`.
- `<kind>` is `fifo` for a FIFO queue (a Redis LIST) or `prio` for a priority queue (a Redis ZSET). A FIFO and a priority queue with the same queue key are different queues.
- `<queue key>` is the name the application gives the queue. It MUST NOT be empty and MUST NOT contain `{` or `}`.

The braces are a Redis Cluster hash tag. Every key of a queue is the base key plus a suffix, so all of them hash on `<queue key>` and live in one slot. This lets each script below touch all of a queue's keys atomically on a cluster. An implementation MUST pass every key a script touches in `KEYS`, and MUST NOT build key names inside a script.

The Queue ID that an implementation reports (`QueueInspector.QueueID` in Go, `id` in the HTTP API) is the base key.

### 1.2 All keys of a queue

`<base>` is the base key. Times marked "s" are Unix seconds as a decimal number with a fraction. Times marked "ms" are integer Unix milliseconds. All times come from the Redis clock (`TIME`) inside a script, except `enqueued_at`, which the producer writes.

| Key | Type | Contents | Since |
|---|---|---|---|
| `<base>` (fifo) | LIST | Items, oldest at the head. Producers push to the tail; consumers pop from the head; requeues push to the head. | redisq |
| `<base>` (prio) | ZSET | Score: the priority, lower first. Member: the byte `0xFF`, then the sequence number as 16 lowercase hex digits, then the item. A member that does not start with `0xFF` is a bare item written by an older version. | redisq |
| `<base>:seq` | STRING | Prio only. The last sequence number, from `INCR`. It orders equal priorities by arrival. | redisq |
| `<base>:nacked` | HASH | Processing jobs: ack ID → item, without the sequence prefix. | redisq |
| `<base>:timeouts` | ZSET | Ack ID → visibility deadline (s). Absent when the visibility timeout is 0. | redisq |
| `<base>:prio` | HASH | Prio only. Ack ID → the score the item had, so a requeue restores its priority. | redisq |
| `<base>:idle` | SET | IDs of distributed consumers waiting for work. | redisq |
| `<base>:heartbeats` | ZSET | Owner → time its heartbeat runs out (s). | redisq |
| `<base>:heartbeats:clock` | STRING | Time of the last sweep by any owner (s). | redisq |
| `<base>:presence` | ZSET | Owner → 0 while it is seen subscribed to its wake channel, else the time it was first seen without a subscriber (s). | redisq |
| `<base>:dedup:<job id>` | STRING | `1`, with a `PX` expiry equal to the dedupe window. Its presence means a job with this ID was pushed within the window. | redisq |
| `<base>:started` | HASH | Ack ID → time the attempt started (ms). | 1.0.0 |
| `<base>:attempts` | HASH | Job ID given by the producer → number of times the job went back to the queue without an outcome. | 1.0.0 |
| `<base>:jobs` | HASH | Job ID → Job Record (section 4), as JSON. | 1.0.0 |
| `<base>:completed` | ZSET | Job ID → time the job completed (ms). | 1.0.0 |
| `<base>:failed` | ZSET | Job ID → time the job failed (ms). | 1.0.0 |
| `<base>:stats` | HASH | Counters `submitted`, `completed`, `failed`. A missing field is 0. | 1.0.0 |
| `<base>:paused` | STRING | Present while the queue is paused: the time it was paused (ms). No expiry. | 1.3.0 |

Pub/sub channel `<base>:wake:<owner>` carries the message `enqueued`. Each owner subscribes to its own channel while it is open, and other owners use the subscriber count to tell that it is alive.

Since 1.3.0, pub/sub channel `<base>:control` carries `paused:<ms>` and `resumed` (sections 6.7, 6.8).

## 2. Ack IDs and owners

An owner ID is a random string of at least 128 bits that contains no `:`. An owner creates a new one each time it opens.

An ack ID is:

```
<owner>:<reservation>:<index>
```

`<reservation>` is a counter of the owner's pop calls, as 20 decimal digits with leading zeros. `<index>` is the position of the item in that call, from 1, as 4 decimal digits. Because both parts have fixed widths, the ack IDs of one owner sort in the order they were handed out.

A script finds the owner of an ack ID as the part before the first `:`.

## 3. Items: the job envelope

An item is a UTF-8 JSON object, the job envelope:

```json
{"id": "order-42", "status": "Created", "data": {"sku": "A1", "qty": 2}, "enqueued_at": 1762228800000}
```

| Field | Type | Rule |
|---|---|---|
| `id` | string | The Job ID the producer gave, or `""` for none. |
| `status` | string | One of `Created`, `Queued`, `Processing`, `Finished`, `Closed`. A producer MUST write `Created`. A consumer MUST NOT run a job whose status is `Closed`. A consumer MUST reject an item with any other value as unparsable. |
| `data` | any JSON | The payload. |
| `enqueued_at` | integer, optional | Since 1.0.0. Time the producer added the job (ms), from the producer's clock. A producer SHOULD write it. |

A consumer MUST ignore fields it does not know.

**An implementation MUST NOT decode and re-encode an item.** It stores, moves and records the bytes as the producer wrote them. Redis's Lua `cjson.encode` writes numbers with 14 significant digits, so a re-encoded payload can lose precision. A script MAY decode an item with `cjson.decode` to read `id`. It builds any JSON that contains an item by concatenating strings around the raw item bytes.

An item that is not a JSON object (redisq can also hold plain bytes that no varmq producer wrote) has Job ID `""` and no `data`. Section 6 says how a reader shows it.

### 3.1 Job ID

The Job ID of a job is:

- the envelope's `id`, when it is not empty;
- otherwise, the ack ID of the attempt that is running or that finished the job.

A pending job whose envelope `id` is empty has no Job ID yet. The HTTP API shows it as `""`.

A producer that reuses an `id` after the dedupe window ends gets a new job with the same Job ID. Its Job Record replaces the older one.

## 4. Job Record

When a job completes or fails, storage writes one Job Record under its Job ID in `<base>:jobs`, if retention keeps that state (section 5). The record is a JSON object:

| Field | Type | Contents |
|---|---|---|
| `id` | string | The Job ID. |
| `state` | string | `completed` or `failed`. |
| `item` | any JSON | The item, verbatim. If the item is not valid JSON, the item as a JSON string. |
| `started_at` | integer, optional | Time the last attempt started (ms), from `<base>:started`. Absent if not known. |
| `finished_at` | integer | Time the job completed or failed (ms). The same value as its score in `:completed` or `:failed`. |
| `attempts` | integer | 1 plus the job's count in `<base>:attempts`. 1 when the envelope `id` is empty. |
| `errors` | array, optional | The job's failed attempts, oldest first, at most the last 10: each `{"attempt": <integer>, "at": <ms>, "error": <string>}`. A writer that adds an 11th entry drops the oldest. Absent on a completed job. |

`enqueued_at` is not copied into the record: a reader takes it from `item.enqueued_at`. A reader MUST ignore fields it does not know, so later versions can add fields.

A script writes integers with `string.format('%.0f', n)`, never with `tostring` or `cjson.encode`, so that no number is written in exponent form. It writes strings with `cjson.encode(s)`, which is exact for strings.

Example:

```json
{"id":"order-42","state":"failed","item":{"id":"order-42","status":"Created","data":{"sku":"A1","qty":2},"enqueued_at":1762228800000},"started_at":1762228801250,"finished_at":1762228801900,"attempts":2,"errors":[{"attempt":2,"at":1762228801900,"error":"panic recovered inside worker: out of stock"}]}
```

## 5. Retention

Retention decides how many Job Records storage keeps. It is set on each queue object, not in Redis. All instances of a queue SHOULD use the same values; each trim applies the values of the instance that runs it.

| State | Most records | Longest age |
|---|---|---|
| completed | 1,000 | 24 hours |
| failed | 10,000 | 7 days |

These are the defaults. An application MAY change each of the four limits. When either limit of a state is 0, storage keeps no records of that state: it writes none, and the next trim removes the ones that exist. The `:stats` counters count every job, whatever the retention.

**Trim** of a state with set `Z` (`:completed` or `:failed`), most records `N`, longest age `A` (ms) and current time `now` (ms):

1. Collect the members of `Z` with a score at or below `now - A` (`ZRANGEBYSCORE Z -inf <now-A>`). If `A` is 0, collect all members.
2. Collect the members outside the newest `N` (`ZRANGE Z 0 -(N+1)`). If `N` is 0, collect all members.
3. For each collected member: `ZREM Z member` and `HDEL <base>:jobs member`.

A trim MUST remove the record from `:jobs` together with its set member. It MUST collect members before it removes them, so that it knows which `:jobs` fields to delete. An implementation MAY limit how many members one trim removes, to keep each script short; the next trim removes the rest.

The Acknowledge and Reject scripts trim the state they write. An implementation SHOULD also trim both states from time to time, for example in its once-a-second sweep, so that records of an idle queue still age out. A reader MAY show records past the age limit until the next trim.

## 6. Operations

Each operation below MUST run as one Lua script (`EVAL` / `EVALSHA`), so that Redis applies it atomically. Scripts that read `TIME` and then write MUST call `redis.replicate_commands()` first; Redis 6 rejects such writes otherwise.

"Now (ms)" means `t = redis.call('TIME')`, then `t[1] * 1000 + math.floor(t[2] / 1000)`.

### 6.1 Enqueue

Push the item: `RPUSH <base>` for fifo; `INCR <base>:seq` and `ZADD <base> <priority> <0xFF><seq as 16 hex><item>` for prio. Then, in the same script:

- `HINCRBY <base>:stats submitted 1`.

With deduplication (a job with a non-empty `id` and a dedupe window above 0): if `<base>:dedup:<id>` exists, the script does nothing and does not count the job. Otherwise it pushes, counts, and sets `<base>:dedup:<id>` to `1` with `PX <window>`, after the push.

A distributed queue also wakes idle consumers in the same script (`SPOP <base>:idle`, then `PUBLISH <base>:wake:<consumer> enqueued`, up to one per waiting item).

### 6.2 Pop

Every script that moves items from the queue to processing (one item or a batch, plain or distributed) does, for each item it hands out under a new ack ID:

- `HSET <base>:nacked <ack id> <item>`, with the sequence prefix removed for prio;
- `ZADD <base>:timeouts <deadline (s)> <ack id>` when the visibility timeout is above 0;
- `HSET <base>:prio <ack id> <score>` for prio;
- since 1.0.0, `HSET <base>:started <ack id> <now (ms)>`.

A pop that runs again with the same ack IDs (a client re-sent the command after a lost reply) finds them in `:nacked` and hands those items back. It MUST NOT pop new items or change `:started`.

Since 1.3.0, after that re-send check, a pop MUST check `EXISTS <base>:paused`. While the key exists the pop hands out nothing, does not add the consumer to `<base>:idle`, reports 0 items left, and tells its caller that the queue is paused, so the caller stops popping until a Resume (section 6.8). This holds for every pop, plain or distributed, one item or a batch.

### 6.3 Acknowledge (completed)

Input: the ack ID. Steps:

1. `item = HGET <base>:nacked <ack id>`. If it is absent (already acknowledged, rejected or requeued), stop and report success. Write no record and count nothing.
2. Remove the job from processing: `HDEL <base>:nacked`, `ZREM <base>:timeouts`, `HDEL <base>:started` (after reading it) for the ack ID; and `HDEL <base>:prio` for prio.
3. Find the envelope `id` (`pcall(cjson.decode, item)`, then take `id` if it is a string). The Job ID is that `id`, or the ack ID when it is empty.
4. `attempts = 1 + (HGET <base>:attempts <id> or 0)`, then `HDEL <base>:attempts <id>`; both only when the envelope `id` is not empty.
5. `HINCRBY <base>:stats completed 1`.
6. `ZREM <base>:failed <job id>`, so a reused Job ID moves to the newest state. Then, if retention keeps completed records: `HSET <base>:jobs <job id> <record>` and `ZADD <base>:completed <now (ms)> <job id>`; if not: `HDEL <base>:jobs <job id>`.
7. Trim `:completed` (section 5).

### 6.4 Reject (failed)

Input: the ack ID and a reason string. The steps are those of Acknowledge, with these differences:

- step 5 is `HINCRBY <base>:stats failed 1`;
- step 6 removes the Job ID from `<base>:completed`. Then, if retention keeps failed records, it writes the record, with `state` `failed` and one `errors` entry (`attempt` = `attempts`, `at` = now (ms), `error` = the reason), to `<base>:failed`; if not, it does `HDEL <base>:jobs <job id>`;
- step 7 trims `:failed`.

Reject never puts the item back in the queue.

A consumer calls Acknowledge when a job succeeds and Reject when it fails for the last time. A consumer that retries a failed job calls neither: the job stays processing until the consumer finishes it or the storage requeues it. (varmq has no retries in this version, so every failure calls Reject.)

redisq plans for a failed attempt with retries left to bump `attempts` and save its error before the requeue. That needs an operation that records a failed attempt without finishing the job, and 1.0.0 has none. Until a later version adds it, `errors` holds only the Reject entry, and `attempts` counts only hand-outs.

### 6.5 Requeue

Every script that moves an item from processing back to the queue (the visibility timeout, a dead or gone owner found by the sweep, an owner's release on close, and a manual requeue) does, for each item it moves:

- push it back: `LPUSH <base>` for fifo; for prio, `INCR <base>:seq` and `ZADD` at the score in `<base>:prio` (1 if none), then `HDEL <base>:prio`;
- `HDEL <base>:nacked`, `ZREM <base>:timeouts`;
- since 1.0.0, `HDEL <base>:started <ack id>`;
- since 1.0.0, when the envelope `id` is not empty, `HINCRBY <base>:attempts <id> 1`.

`:attempts` is keyed by Job ID, not by enqueue. Two jobs in the queue at once with the same Job ID (re-added after the dedupe window) share one count, and the first to finish clears it for both.

The script pushes the item before it removes it from `:nacked`, so a failed push loses nothing. A requeue is neither completed nor failed, so it changes no `:stats` counter.

### 6.6 Purge

Purge deletes all of a queue's jobs and job data in one `DEL`:

- the base key and, for prio, `<base>:seq` and `<base>:prio`;
- processing jobs: `<base>:nacked`, `<base>:timeouts`, `<base>:started`;
- `<base>:attempts`;
- Job Records: `<base>:jobs`, `<base>:completed`, `<base>:failed`;
- the counters: `<base>:stats`.

Purge keeps the consumer state that live consumers rely on (`<base>:idle`, `<base>:heartbeats`, `<base>:heartbeats:clock`, `<base>:presence`) and the `<base>:dedup:<id>` keys, which expire on their own. A consumer that later acknowledges or rejects a purged job finds no `:nacked` entry, so the operation does nothing (section 6.3, step 1).

### 6.7 Pause (since 1.3.0)

Input: none. One script:

1. `SET <base>:paused <now (ms)> NX`. Pausing a paused queue keeps the first time.
2. If the `SET` stored the key, `PUBLISH <base>:control paused:<now (ms)>`.

Pause stops hand-outs only. Enqueue, Acknowledge, Reject and every Requeue (section 6.5) work as before; requeued items wait in the queue. Purge (section 6.6) and any expiry a client sets on the queue's keys leave `<base>:paused` alone: only Resume ends a pause.

### 6.8 Resume (since 1.3.0)

Input: none. One script:

1. `DEL <base>:paused`. If it deleted nothing, stop.
2. Wake idle consumers as Enqueue does (section 6.1), up to one per waiting item.
3. `PUBLISH <base>:control resumed`.

A consumer SHOULD subscribe to `<base>:control` and update its own view of the pause on each message. Pub/sub can lose a message, so a consumer SHOULD also read `EXISTS <base>:paused` at least once a second, and before its first pop.

## 7. Reading

A reader (in Go, a `QueueInspector`) answers the two HTTP routes as follows. It SHOULD read each answer in one script or `MULTI` transaction, so that the numbers come from one instant.

A reader MUST accept a Job State it does not know, in a Job Record or from the HTTP API, and show the value as it is. It MUST NOT treat it as an error. `cancelled` is reserved for a later version. This rule lets a later version add a state as a minor change.

### 7.1 Counts and totals (`GET /queues`)

| Field | Source |
|---|---|
| `counts.pending` | `LLEN <base>` (fifo) or `ZCARD <base>` (prio) |
| `counts.processing` | `HLEN <base>:nacked` |
| `counts.completed` | `ZCARD <base>:completed` |
| `counts.failed` | `ZCARD <base>:failed` |
| `totals.submitted`, `totals.completed`, `totals.failed` | `HMGET <base>:stats submitted completed failed`, a missing field as 0 |
| `paused`, `paused_at` | Since 1.3.0: `GET <base>:paused`. Absent: `paused` false, no `paused_at`. Present: `paused` true, `paused_at` its value. |

`counts.completed` and `counts.failed` are the records retention keeps. The totals count every job since the counters began.

### 7.2 Jobs (`GET /queues/{id}/jobs`)

Page bounds: `start = offset`, `stop = offset + limit - 1`.

- **pending**: `LRANGE <base> start stop`, or `ZRANGE <base> start stop` with the sequence prefix removed. For each item, the Job ID is the envelope `id`; `attempts` is `HGET <base>:attempts <id>`, or 0. No `started_at`.
- **processing**: all fields of `<base>:nacked`, sorted by ack ID, then the page. The Job ID follows section 3.1. `attempts = 1 + (HGET <base>:attempts <id> or 0)`. `started_at` is `HGET <base>:started <ack id>`.
- **completed**, **failed**: `ZREVRANGE <base>:<state> start stop`, then `HMGET <base>:jobs` for those members. Skip a member whose record is gone (a trim ran between the two reads).

Mapping to the HTTP `Job`: `payload` is the envelope's `data`; `enqueued_at` is the envelope's `enqueued_at`; the other fields come from the record or as above. For an item that is not an envelope, `payload` is the item as a JSON string (or the item itself, if it is valid JSON) and `enqueued_at` is absent.

## 8. Instance Records

An Instance Record tells a reader that a varmq process is alive and what its workers are doing. Writing records is optional. A process writes them only when the application turns this on. A reader MUST treat a missing record as "unknown", not as an error, because an instance on an older version writes none.

### 8.1 Keys

These keys belong to a namespace, not to a queue:

| Key | Type | Contents | Since |
|---|---|---|---|
| `redisq:[<namespace>:]{instances}` | HASH | Instance ID → Instance Record, as JSON | 1.1.0 |
| `redisq:[<namespace>:]{instances}:beats` | ZSET | Instance ID → time of its last report (ms) | 1.1.0 |

`<namespace>` follows section 1.1. The braces are a hash tag, so both keys live in one slot and one script can touch both on a cluster. They cannot collide with a queue key: a queue key has `fifo` or `prio` after the namespace, and a namespace cannot contain braces. A queue's Purge (section 6.6) does not touch them.

The **Instance ID** names one process. It is the value the process sends in the `X-Varmq-Instance` header (`spec/openapi.yaml`), so a reader of this section and a reader of the HTTP API name a process the same way. A writer reads the ID once, when its reports start; if the process changes its ID later, the header and the record differ until the process restarts, so a process SHOULD set its ID before it starts reports. It MUST NOT be empty. Two processes that report to one namespace MUST NOT use the same ID. varmq's default ID is the hostname, so two processes on one host MUST each set their own ID.

### 8.2 Record

The record is a JSON object:

| Field | Type | Contents |
|---|---|---|
| `id` | string | The Instance ID. The same as the hash field. |
| `host` | string | The hostname, or empty. |
| `pid` | integer | The process ID, or 0. |
| `version` | string | The varmq version, or empty. |
| `started_at` | integer | Time the process started (ms), from the process clock. |
| `interval_ms` | integer | Time between reports. MUST be from 1000 to 60000. |
| `workers` | array | One entry per worker, below. Empty when the process has no workers. |
| `owners` | array of strings, optional | Owner IDs (section 2) of the queue objects this instance has open in this namespace. |

Each `workers` entry:

| Field | Type | Contents |
|---|---|---|
| `name` | string | The worker's name, as in `GET /workers`. |
| `status` | string | A `WorkerStatus` value from `spec/openapi.yaml`. A reader MUST accept a value it does not know. |
| `concurrency` | integer | Most jobs the worker runs at once. |
| `num_processing` | integer | Jobs the worker is running now. |
| `num_idle_workers` | integer | Pool goroutines waiting for a job. |
| `completed`, `successful`, `failed` | integer | Since 1.2.0. Jobs this worker finished in this process: all, without error, with an error or panic. Absent from a 1.1.0 writer; a reader takes absent as 0. How many jobs a queue received is the queue's `submitted` total (section 7.1), not a worker's. |
| `queue_ids` | array of strings | Queue IDs (section 1.1) of the queues the worker is bound to. Empty for a queue with no Queue ID, such as an in-memory queue. |

The record has no time of the last report, and a writer MUST NOT add a `beat_at` or `lost` field. That time is the member's score in `:beats`, from the Redis clock, so that the clocks of the processes do not matter. A reader that serves records (in Go, `InstanceRecord`) sets `beat_at` from the score and `lost` from section 8.5. A reader MUST ignore fields it does not know.

`owners` links a processing job to its process: the ack ID of a processing job starts with its owner (section 2), and the record that lists that owner is the instance that holds the job. A writer MAY leave `owners` out.

### 8.3 Report

An instance reports at start and then once every `interval_ms`. Since 1.2.0 it SHOULD also report at once on a lifecycle change: a worker starts or stops, is paused or resumed, has its concurrency changed, or leaves the process. Load changes (`Idle` to `Running` and back, busy and processed counts) wait for the next interval. Input: the Instance ID and the record. One script, under the rules for scripts in section 6:

1. now (ms) (section 6).
2. `HSET <instances> <id> <record>` and `ZADD <instances>:beats <now (ms)> <id>`.
3. Prune: `stale = ZRANGEBYSCORE <instances>:beats -inf (<now - 300000> LIMIT 0 100`. For each member, `HDEL <instances>` and `ZREM <instances>:beats`.
4. `PEXPIRE <instances> 300000` and `PEXPIRE <instances>:beats 300000`.

`<instances>` is `redisq:[<namespace>:]{instances}`. The prune age of 5 minutes is more than 3 times the longest interval, so it never removes a live record. The limit keeps each script short; the next report removes the rest. Step 4 cleans up when no reporter is left: if no instance reports for 5 minutes, Redis deletes both keys.

### 8.4 Remove

Input: the Instance ID. `HDEL <instances> <id>` and `ZREM <instances>:beats <id>`, in one script or `MULTI`. An instance SHOULD remove its record when it stops cleanly. An instance that stops in any other way leaves its record until a prune.

### 8.5 Reading

A reader reads `TIME`, `ZRANGE <instances>:beats 0 -1 WITHSCORES` and `HGETALL <instances>` in one script or `MULTI`. It skips an ID that has a score but no record, or a record but no score: a prune or a remove ran between two commands of a writer.

A record is **live** when its score is at least `now - 3 × interval_ms`, with now from the same `TIME`. Otherwise it is **lost**: the process probably stopped without a remove. A reader SHOULD show a lost record as lost, not hide it. A reader MUST skip a record whose score is older than `now - 300000`: a prune would remove it, and the last reporter may be gone (section 8.3).

## 9. Compatibility

- Items, `:nacked` and the other keys from redisq are unchanged in 1.0.0, so a 1.0.0 consumer reads items that an older producer wrote. Such items have no `enqueued_at`.
- An older producer does not count `submitted`, and an older consumer writes no records and counts nothing. While old and new instances share a queue, the totals and records cover only the new ones.
- An older requeue does not `HINCRBY <base>:attempts` or `HDEL <base>:started`. A job it requeues gets a too-low `attempts` in its record, and its `:started` entry stays until Purge.
- 1.3.0 adds a key, a channel and a pop rule. A pre-1.3.0 consumer ignores `<base>:paused` and keeps popping, so a queue is paused only once every consumer of it runs 1.3.0. For varmq that means every consumer process runs a varmq with queue pause and a storage on 1.3.0: a varmq without it, on a 1.3.0 storage, keeps asking a paused queue for work and gets none.
- 1.2.0 adds worker fields and a reporting rule only. A 1.1.0 reader ignores the new fields.
- 1.1.0 adds keys and a reader rule only. A 1.0.0 implementation works unchanged beside a 1.1.0 one. An instance on 1.0.0 writes no Instance Record, so a reader that lists instances from section 8 does not see it.
- A new field in an item, a Job Record or an Instance Record is a minor version. A new Job State is a minor version too (section 7). A changed meaning, or a removed or renamed key or field, is a major version.
