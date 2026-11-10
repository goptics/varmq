# VarMQ Redis storage

**Version 1.9.0**

This document is the contract for a varmq queue kept in Redis. redisq is its Go implementation. A TypeScript or Rust implementation that follows it can produce to, consume from and inspect the same queue as redisq, and `spec/openapi.yaml` describes how the result reaches observers. A change to anything below starts here, with a version bump.

The words MUST, MUST NOT, SHOULD and MAY are used as in RFC 2119.

## Changes

- **1.9.0**: workflow counts and task times (sections 1.2, 6.6, 6.9 to 6.12, 7.3): sorted set `wf:index:states` (workflow ID → state number), written with every `meta.state` write; hash fields `ea:<task>`, `sa:<task>` and `fa:<task>`, and `u:<task>` members `enqueued_at`, `started_at` and `finished_at`; Runnable is one script that stamps the start on a yes. Nothing from 1.8.0 changes for a reader that ignores them.
- **1.8.0**: FinishTask (section 6.10) is an operation of the workflow store, not of the queue: the caller passes the queue ID and the ack ID of the item. A store registers itself by scope, so a queue only names its scope, and a consumer finds the store by that scope. No key, script or item changes: nothing from 1.7.0 changes for the data in Redis.
- **1.7.0**: job cancel (section 6.14): sorted set `<base>:cancelled`, control message `cancel:<job id>`, record and outcome state `cancelled`. Nothing from 1.6.0 changes while the set is absent.
- **1.6.0**: job outcome (section 6.13): channel `<base>:done:<job id>`, Acknowledge with a result, Job Record field `result`. Nothing from 1.5.0 changes for a reader that ignores them. redisq offers only distributed queues (every enqueue wakes idle consumers); the plain behaviour stays valid for other implementations.
- **1.5.0**: workflows (sections 6.9 to 6.12, 7.3): keys `wf:<id>` and `wf:index`, envelope fields `wf`, `task` and `undo`, Purge cancels the workflow tasks it removes. 1.4.0 is reserved for retries (separate change). Nothing from 1.3.0 changes for a job without `wf`.
- **1.3.0**: queue pause (sections 6.7, 6.8): key `<base>:paused` and channel `<base>:control`; every pop checks the key. Nothing from 1.2.0 changes while the key is absent.
- **1.2.0**: a worker entry gains `completed`, `successful` and `failed`; an instance reports at once on a lifecycle change (section 8.3). Nothing from 1.1.0 changes.
- **1.1.0**: adds Instance Records (section 8), new keys only, and the reader rule for unknown Job States (section 7). Nothing from 1.0.0 changes.
- **1.0.0**: first version. Describes the key layout redisq already uses, and adds Job Records, retention, the `:stats` counters, the `:started` and `:attempts` hashes, the `enqueued_at` envelope field and the `Reject` operation. Purge deletes all job data, as redisq PR #28 does, plus the new keys.

## Terms

Terms:

- **Queue ID**: the identity a storage reports for a queue, the same on every instance. Here it is the queue's base key (below), for example `redisq:shop:fifo:{emails}`.
- **Job State**: `pending` (in the queue), `processing` (handed out to a consumer, no outcome yet), `completed` (acknowledged) or `failed` (rejected). Since 1.5.0 a workflow task can also be `waiting` or `cancelled` (section 7.3). Since 1.7.0 any job can be `cancelled` (section 6.14). Later versions can add states (section 7).
- **Reject**: the final failure of a job. No retry follows, and the job is not requeued.
- **Job Record**: what storage keeps about a completed or failed job after it leaves the queue.

Two more terms are used here:

- **Workflow**: a DAG of tasks, each a job bound for a queue, kept in one hash (section 1.2). **Task**: one named job of a workflow. A task waits in the workflow hash until its dependencies end, then storage pushes it into its queue in the same script that ended the last of them. **Undo**: a task's compensation job. Section 6.9 defines the rules.
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

Workflow keys belong to a namespace, not to a queue (section 6.9):

| Key | Type | Contents | Since |
|---|---|---|---|
| `redisq:[<namespace>:]wf:<id>` | HASH | One workflow. Fields below. Expires after the retention time once the workflow has ended. | 1.5.0 |
| `redisq:[<namespace>:]wf:index` | ZSET | Workflow ID → time it was created (ms). | 1.5.0 |
| `redisq:[<namespace>:]wf:index:states` | ZSET | Workflow ID → the number of its state: 1 `running`, 2 `completed`, 3 `failed`, 4 `compensating`, 5 `compensated`, 6 `compensation_failed`, 7 `cancelled`; 0 for an index entry that had no score and whose hash is gone or unreadable (section 7.3, Counts). Same members as `wf:index`. | 1.9.0 |

`<id>` is 1 to 128 characters of `[A-Za-z0-9_.-]` and is not `index`. No ID holds a `:`, so `wf:index:states` is never a workflow hash. A task name is 1 to 64 characters of the same set. Fields of `wf:<id>`, where `<task>` is a task name:

| Field | Value |
|---|---|
| `meta` | JSON: `state`, `created_at` and `finished_at` (ms, the latter absent while running), `fail_fast`, `undo_on_cancel` (Submit's option; a Cancel that gives `undo` overwrites it, section 6.11), `cancelled` (a cancel was called), `failed` (an unabsorbed failure happened before any cancel), `hash` (the plan hash), and `tasks`, the task names in Add order (a topological order). |
| `t:<task>` | JSON, written at Submit and never changed: the queue's base key and kind (`fifo` or `prio`), priority, `deps`, `dependents` (names, in Add order), `always` (the dependency policy), the job ID, and `undo` (the undo's queue base key, kind, job ID and envelope, embedded verbatim; a prio undo is pushed at priority 0) or absent. |
| `s:<task>` | The task's state: `waiting`, `pending`, `completed`, `failed` or `cancelled`. A pop does not write it, so it stays `pending` while the task is queued or processing. |
| `n:<task>` | Integer: dependencies not ended yet. Present while the task is `waiting`. |
| `i:<task>` | The task's envelope, until it is pushed. |
| `r:<task>` | The task's Result bytes, a JSON document. Present when the task completed with a result. |
| `e:<task>` | The last failure reason, a string. |
| `u:<task>` | JSON `{"state": <state>, "open": <integer>, "reason": <string>}`: the undo's state (`waiting`, `pending`, `completed` or `failed`), the undos of its completed descendants not ended yet, and, only when the undo ended `failed`, the reason of its end (absent when empty). Absent until compensation marks the undo. Since 1.9.0 also `enqueued_at` (the undo was pushed), `started_at` (Runnable last said yes, section 6.12) and `finished_at` (the undo ended): integers, ms, each absent until reached. |
| `ea:<task>` | Since 1.9.0. Time (ms) the task was pushed to its queue: written with every `s:<task>` write of `pending` (Submit for a root, else the script that released it). |
| `sa:<task>` | Since 1.9.0. Time (ms) Runnable last said yes for the task (section 6.12). |
| `fa:<task>` | Since 1.9.0. Time (ms) the task ended: written with every `s:<task>` write of `completed`, `failed` or `cancelled`. A task cancelled while `waiting` has only this one. |

The three time fields hold a decimal integer of the Redis clock (`TIME`), as every "now" of a script. A time that was not reached has no field (never 0).

An implementation MUST NOT write any other field name that starts with one of these prefixes. A writer builds JSON that contains an envelope by concatenating strings around its bytes (section 3).

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
| `wf` | string, optional | Since 1.5.0. The workflow ID, on the item of a workflow task or undo only. |
| `task` | string, optional | Since 1.5.0. The task name within `wf`. A producer that writes `wf` MUST write `task`. |
| `undo` | boolean, optional | Since 1.5.0. `true` on the undo job of `task`; absent otherwise. |

A consumer MUST ignore fields it does not know.

Since 1.5.0, a consumer MUST treat an item as unparsable when its workflow fields could not come from a Submit (section 6.9): `wf` that is not a workflow ID of section 1.2, `task` that is not a task name, `wf` without `task`, `task` or `undo` without `wf`, or an `id` other than `wf:<wf>:<task>` (with `:undo` appended when `undo` is true). It MUST NOT build a `wf:` key from such an item, and MUST NOT run it.

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
| `result` | any JSON, optional | Since 1.6.0: the result an Acknowledge with result left (section 6.13), verbatim. Absent otherwise. |

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

redisq plans for a failed attempt with retries left to bump `attempts` and save its error before the requeue. That needs an operation that records a failed attempt without finishing the job, and this version has none. Until a later version adds it, `errors` holds only the Reject entry, and `attempts` counts only hand-outs.

### 6.5 Requeue

Every script that moves an item from processing back to the queue (the visibility timeout, a dead or gone owner found by the sweep, an owner's release on close, and a manual requeue) does, for each item it moves:

- push it back: `LPUSH <base>` for fifo; for prio, `INCR <base>:seq` and `ZADD` at the score in `<base>:prio` (1 if none), then `HDEL <base>:prio`;
- `HDEL <base>:nacked`, `ZREM <base>:timeouts`;
- since 1.0.0, `HDEL <base>:started <ack id>`;
- since 1.0.0, when the envelope `id` is not empty, `HINCRBY <base>:attempts <id> 1`.

`:attempts` is keyed by Job ID, not by enqueue. Two jobs in the queue at once with the same Job ID (re-added after the dedupe window) share one count, and the first to finish clears it for both.

The script pushes the item before it removes it from `:nacked`, so a failed push loses nothing. A requeue is neither completed nor failed, so it changes no `:stats` counter.

### 6.6 Purge

Purge deletes all of a queue's jobs and job data in one `DEL`. Since 1.5.0 the same script then cancels the workflow tasks the `DEL` removed (below).

- the base key and, for prio, `<base>:seq` and `<base>:prio`;
- processing jobs: `<base>:nacked`, `<base>:timeouts`, `<base>:started`;
- `<base>:attempts`;
- Job Records: `<base>:jobs`, `<base>:completed`, `<base>:failed`;
- the counters: `<base>:stats`.

Purge keeps the consumer state that live consumers rely on (`<base>:idle`, `<base>:heartbeats`, `<base>:heartbeats:clock`, `<base>:presence`) and the `<base>:dedup:<id>` keys, which expire on their own. A consumer that later acknowledges or rejects a purged job finds no `:nacked` entry, so the operation does nothing (section 6.3, step 1).

Since 1.5.0, a purged item that names a workflow (`wf` and `task`) would leave the workflow waiting for a task that is gone. So Purge reads the queued items (`LRANGE` or `ZRANGE`) and `HVALS <base>:nacked` first and collects the distinct `wf` IDs. It MUST ignore an item whose workflow fields are invalid (section 3): such an item is purged like a plain job, and no `wf:` key is built from it. It runs its script with, in `KEYS`: the keys the `DEL` removes, then for each such workflow its hash `wf:<id>`, then (since 1.9.0) `wf:index:states`, then the push-target queues of section 6.10 (every queue the workflow can push into). The script runs the `DEL` first. Then, for each tagged item read, in the order read, it applies a `cancelled` end to the task (or to its undo, for `undo` true) with steps 4 to 6 of section 6.10, with no `:nacked` check and no settle (the `DEL` removed the item and its records), so a dependent or undo that the end releases is pushed after the `DEL` and is not lost with it. Dependents follow their policy, and a purged task is never recorded as completed. A queue with no workflow item pays one extra scan only. The script MUST re-read the queued items and `:nacked` before the `DEL` and, if it finds a `wf` ID whose hash is not in `KEYS` (an item arrived since the caller read), do nothing and tell the caller to read again. The caller MUST read again at most 3 times, then fail the Purge, so a queue that keeps receiving workflow items cannot hold it in a loop.

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

A consumer SHOULD subscribe to `<base>:control` and update its own view of the pause on each message. Pub/sub can lose a message, so a consumer that keeps a view of the pause MUST also read `EXISTS <base>:paused` before its first pop and at least once a second after, and on a change wake itself as on a message.

The key decides; messages are hints. A consumer MUST ignore a `<base>:control` message it does not know, and MUST NOT use the `<ms>` of a `paused:` message for anything but display. A pop checks the key itself (section 6.2), so a lost or forged message costs at most one empty pop or one second of waiting.

### 6.9 Submit a workflow (since 1.5.0)

A workflow is a DAG of tasks fixed at Submit: no task is added later. Each task has a name, a queue, an envelope, dependencies (earlier tasks, so the DAG has no cycle), a dependency policy and optionally an undo. All queues of one workflow MUST live in one Redis instance and one namespace; a script that touches the keys of several queues needs that. (This version does not support Redis Cluster: queue keys hash on different slots.)

Input: the workflow ID, its options (`fail_fast`, `undo_on_cancel`), the plan hash, and per task the data of `t:<task>` and its envelope. The job ID of a task is `wf:<id>:<task>`, and of its undo `wf:<id>:<task>:undo`.

The plan hash is the lowercase hex SHA-256 of this byte stream, so every implementation produces the same hash for the same plan. `str(x)` is the unsigned LEB128 (uvarint) length of the UTF-8 bytes of `x`, then the bytes. `field(b)` is the same for raw bytes. `flag(v)` is one byte, 1 for true and 0 for false. `num(n)` is the signed varint (zigzag, then uvarint, as Go's `binary.AppendVarint`) of `n`. In order: `num(task count)`; then for each task in Add order: `str(name)`, `str(queue ID)`, `flag(has priority)`, `num(priority)` (0 when it has none), `num(dependency count)`, `str(dep)` for each dependency sorted by byte order, `flag(always)`, `flag(has undo)`, and if it has an undo `str(undo queue ID)` and `field(undo data)`, then `field(task data)`; then `flag(fail_fast)` and `flag(undo_on_cancel)`. `data` is the JSON bytes of the envelope's `data` member, not the envelope, so `enqueued_at` does not change the hash. `KEYS`: the workflow hash, `wf:index`, `wf:index:states` (since 1.9.0), then per distinct queue of a root task its base key, `:stats`, `:idle` and, for prio only, `:seq`. One script:

1. If the hash exists: stop. Return 1 when its `meta.hash` equals the input hash (a repeated Submit succeeds), else 0 (the caller reports `ErrWorkflowExists`).
2. `HSET` `meta` (`state` `running`, `created_at` now (ms), `cancelled` and `failed` false, and the options, hash and task names of the input), and per task `t:`, `s:` and `n:`. A task with no dependency has `s:` `pending`, no `n:` and (since 1.9.0) `ea:` now; any other has `s:` `waiting`, `n:` its dependency count and `i:` its envelope.
3. Push each root task as Enqueue does (section 6.1), in task order: its envelope into its queue, `submitted` counted, idle consumers woken. Deduplication does not apply: a task job ID is unique to its workflow.
4. `ZADD wf:index <now (ms)> <id>`. Since 1.9.0 also `ZADD wf:index:states 1 <id>`.

A caller MUST write the envelope of a task with `status` `Created`, and with `wf` and `task` (and `undo` for an undo job). It MUST refuse an empty plan, a name or ID outside the sets of section 1.2, and a queue that is not in the workflow's scope.

### 6.10 FinishTask (since 1.5.0)

A consumer that takes an item whose envelope has `wf` MUST finish it with this operation, not with Acknowledge or Reject (section 6.3, 6.4). It settles the item as they do and applies the outcome to the workflow in the same script, so no moment exists where a task has ended and its dependents are neither waiting nor queued.

The operation belongs to the workflow store, which a process makes for the storage and registers by scope. A consumer finds the store of the scope of the queue the item came from, and calls it with that queue's Queue ID and the ack ID. A consumer that has no store for the scope MUST NOT run the item and MUST NOT settle it: the item stays pending and is redelivered, and the consumer SHOULD report the missing store.

Input: the Queue ID of the queue the item came from, the ack ID and a task end: workflow, task, `undo`, outcome (`completed`, `failed` or `cancelled`), reason, result bytes; and the result limit (default 65536 bytes) and retention (default 7 days) of the store.

The caller reads the static `t:` fields of the workflow before the script (`t:` never changes after Submit, so a caller MAY cache them) and passes in `KEYS`, in this order:

1. this queue's keys for section 6.3 or 6.4: `<base>:nacked`, `:timeouts`, `:started`, `:prio`, `:attempts`, `:stats`, `:jobs`, `:completed`, `:failed`;
2. the workflow hash `wf:<id>` and, since 1.9.0, `wf:index:states` after it;
3. the **push-target queues**: every queue the workflow can push into, which is the queue of every task and of every undo, each once, including the finishing task's own queue. Per queue: its base key, `:stats`, `:idle`, and `:seq` for prio only.

The caller MUST also check, from those `t:` fields, that `t:<task>` exists and that its queue (for an undo, the queue of `t:<task>.undo`) has this queue's base key. If not (an item written into a queue its workflow does not use), it settles the item (step 3), applies no workflow step, and returns success (ok=true) with no queue IDs. An item that fails section 3's workflow rules is unparsable and never reaches FinishTask.

Every queue is passed, not only the dependents' queues, because one end can push into any of them: compensation can start in this script and push the undo of a task on a sibling branch. A script MUST NOT build a key name (section 1.1); a script that needs a key that is not in `KEYS` fails and changes nothing.

Steps:

1. If `<base>:nacked` has no `item` for the ack ID, stop and change nothing in the workflow: the task already ended (a re-send, or a requeue that another consumer finished). Return success and, so that a wake lost with the first reply is not lost for good, the queue IDs of all push-target queues. If `wf:<id>` does not exist (the workflow ended and expired), settle the item (step 3), change nothing else, and return success (ok=true) with no queue IDs: a failure would redeliver the item forever. (The Go `WorkflowStore` methods, such as `Record` and `Cancel`, still report `ErrWorkflowNotFound` for it.)
2. Before settling: for a `completed` end that is not an undo, if `len(result)` is above the limit, change the outcome to `failed`, reason `varmq: task result too large`, and no result. (So no `completed` Job Record is written for it.) An undo's and a non-completed end's result is dropped.
3. Settle the item as section 6.3 (outcome `completed`) or 6.4 (`failed` and `cancelled`, with the reason).
4. Apply the end to the workflow with the rules below, writing `s:`, `n:`, `r:` (the result, when the task completed), `e:` (the reason, when it failed or was cancelled), `u:` and `meta`. Since 1.9.0 every `s:` write of `pending` also sets `ea:<task>`, and every one of `completed`, `failed` or `cancelled` sets `fa:<task>`, to now (ms); a `u:` gets `enqueued_at` when it becomes `pending` and `finished_at` when it ends. One script uses one "now" per workflow.
5. Push every task or undo the rules make ready, in task order: its stored envelope (`i:` or the undo's from `t:`) as Enqueue does (section 6.1), `HDEL i:<task>`. A promotion ignores queue capacity. Idle consumers of each pushed queue are woken as Enqueue does. A paused queue (section 6.7) takes the item and holds it.
6. When the workflow reaches a final state, set `meta.state` and `meta.finished_at` and `PEXPIRE wf:<id> <retention>`. `wf:index` keeps the entry until a trim (section 7.3). Since 1.9.0 every write of `meta.state`, `compensating` included, is followed by `ZADD wf:index:states <state number> <id>` (section 1.2) in the same script.
7. Return success and the queue IDs it pushed into. A caller whose queue is not distributed wakes its own consumers of those queues; other consumers see the item at their next resync.

An end for a task that is not `pending` or `processing` (waiting, cancelled or ended), or for a workflow that is not `running`, changes nothing: late and repeated ends are safe. The same holds for an undo end outside `compensating`.

**Transition rules.** These are the rules a Go implementation (varmq's in-memory store and its model) follows; a Redis implementation MUST give the same task states, the same push order and the same final state.

- **Release.** A `completed` task releases each dependent that is `waiting`: `n:` is reduced by 1, and at 0 the dependent becomes `pending` and is pushed.
- **Dependency policy.** A `failed` or `cancelled` task applies to each `waiting` dependent: with `always` true the dependent counts the task as ended (released, so it runs and may see the failure); otherwise it becomes `cancelled` and applies the same rule to its own dependents, down the DAG. A cancelled task is not pushed and has no outcome record.
- **Absorbed failures.** A failure is absorbed when the failed task has dependents and every one has `always` true. A failed task with no dependent, or with a dependent that is not `always`, is an unabsorbed failure: `meta.failed` becomes true, unless `meta.cancelled` is already true (after a cancel, the ends of tasks are not failures). A `cancelled` end before any workflow Cancel (a Purge, a pre-run cancel of a task nobody asked to cancel) counts exactly as a `failed` end for this rule, FailFast and `meta.failed`; only its task state differs (`cancelled`).
- **FailFast.** With `fail_fast`, only the first unabsorbed failure cancels every `waiting` task at once, except the failed task's own `always` dependents, which it keeps; each cancelled task applies the dependency policy to its dependents, so those `always` tasks count it. Later failures do not repeat this. From then on the pre-run check (section 6.12) stops every queued task that is not `always`, so a task pushed before the failure and not yet started ends `cancelled` without running.
- **End of the run.** When no task is `waiting`, `pending` or `processing`, the run is over. If `meta.failed` is true, or `meta.cancelled` and `undo_on_cancel` are both true, compensation starts if any `completed` task has an undo; otherwise the state is `failed` when `meta.failed` is true (even if `meta.cancelled` is too), else `cancelled` when `meta.cancelled` is true, else `completed`. So a cancel after an unabsorbed failure keeps that failure's compensation.
- **Compensation.** The state becomes `compensating`. Each `completed` task with an undo gets `u:<task>` `waiting` with `open` = the number of its transitive descendants that are `completed` and have an undo (tasks without an undo are looked through, so the order is a saga's, the reverse of the DAG). A task with `open` 0 becomes `pending` and its undo is pushed. An undo end (`undo` true) sets `u:` `completed` for outcome `completed` and `failed` with the end's `reason` for any other outcome (a cancelled undo counts as failed), and for each transitive ancestor whose `u:` is `waiting` reduces `open` by 1; at 0 that undo is pushed. A failed undo does not stop the others. When no undo is `waiting`, `pending` or `processing`, the state is `compensated`, or `compensation_failed` if any undo failed. An undo never has an undo.

### 6.11 Cancel a workflow (since 1.5.0)

Input: the workflow ID and, optionally, `undo` (whether compensation also runs). Without `undo`, the workflow's own `meta.undo_on_cancel` (set at Submit) decides. `KEYS`: the workflow hash, `wf:index:states` (since 1.9.0), then the push-target queues of section 6.10 (every queue the workflow can push into). One script:

1. If the hash does not exist, return not found. If `meta.state` is not `running`, or `meta.cancelled` is true, stop: cancelling an ended or cancelled workflow changes nothing.
2. Set `meta.cancelled` true and, only when the caller gave `undo`, `meta.undo_on_cancel` to it. Every `waiting` task becomes `cancelled` (no dependency policy applies: they are all cancelled), with `fa:<task>` now (since 1.9.0).
3. `pending` and `processing` tasks stay as they are. A `pending` task is stopped by the pre-run check (section 6.12) when a consumer reaches it, and ends as `cancelled`; a `processing` task finishes normally. This is why Cancel returns a `running` workflow when tasks are in flight.
4. Apply "End of the run" of section 6.10 and push what it makes ready, as 6.10 steps 5 and 6 (times and `wf:index:states` as there).

A cancel does not remove queued items.

The HTTP route (`PATCH /workflows/{id}/actions/cancel`) MUST only narrow: it passes `undo` false for `undo=false`, no `undo` when the parameter is absent, and MUST refuse `undo=true` (HTTP 400), so a REST caller never runs undos the producer turned off. The Go API (`WorkflowStore.Cancel`) takes either value.

### 6.12 Runnable (since 1.5.0)

The pre-run check. A consumer MUST call it for an item whose envelope has `wf`, after popping it and before running it, and only for an item whose workflow fields pass section 3's rules (one that fails them is unparsable: it is neither checked nor run). Input: workflow ID, task, `undo`. One `HMGET wf:<id> meta t:<task> s:<task> u:<task>` (since 1.9.0 inside one script, `KEYS` the workflow hash, with the stamp below):

- Not found (the hash is gone: the workflow ended and expired): not runnable.
- `undo` false: runnable only when `meta.state` is `running`, `meta.cancelled` is not true, `s:<task>` is `pending`, and not (`meta.fail_fast` and `meta.failed` are true and `t:<task>.always` is false): once FailFast fired (section 6.10), only `always` tasks start.
- `undo` true: runnable only when `meta.state` is `compensating` and `u:<task>.state` is `pending`.

Since 1.9.0 a "runnable" answer stamps the start in the same script, so the check and the stamp are one round trip: `HSET wf:<id> sa:<task> <now (ms)>`, or for `undo` true `started_at` now in `u:<task>` (its other members kept). A later yes for the same task (a redelivered item) overwrites it. A "not runnable" or "not found" answer MUST NOT write, so no hash is made for an unknown or expired ID. The stamp MUST NOT set, change or remove the expiry of the hash.

A consumer that gets "not runnable" MUST finish the item with FinishTask outcome `cancelled` (reason `workflow cancelled`) without running it. When the check fails with an error, it SHOULD run the item: delivery is at least once, and a store that cannot answer cannot finish it either. A job without `wf` skips this check.

### 6.13 Job outcome (since 1.6.0)

A producer waits for a job's end from any process. Only a job with a non-empty Job ID has an outcome.

- **Acknowledge with result.** Input: the ack ID and result bytes. As section 6.3, plus: the script MUST check the result is valid JSON (`pcall(cjson.decode, r)`) and, if not, change nothing and report failure; the completed record gets `result`, the bytes verbatim (never decoded and re-encoded: Lua numbers are doubles).
- **Publish.** Every script that ends a job completed or failed (6.3, 6.4, Acknowledge with result; one item or a batch) MUST, after its record step and whatever retention keeps, `PUBLISH <base>:done:<job id> <outcome>`, where outcome is a JSON object: `id`; `state` (`completed` or `failed`); `error`, the reason, on failed; `result`, verbatim, when there is one. Strings via `cjson.encode`.
- **Re-add.** An Enqueue with a Job ID (once, section 6.1) that pushes the item MUST, in the same script, delete that ID's old record: `HDEL <base>:jobs <id>`, `ZREM <base>:completed <id>`, `ZREM <base>:failed <id>`. A dedupe hit changes nothing. So a waiter never takes an earlier run's outcome for the new job's. A waiter on a job with a caller-chosen ID SHOULD subscribe after the push.
- **Refused result.** A client whose Acknowledge with result fails SHOULD Acknowledge without it, so the job completes with no result instead of being redelivered. A client SHOULD send only strict JSON (cjson also takes `NaN`, `inf`, hex numbers, which other readers refuse).
- **Watch.** A waiter SUBSCRIBEs `<base>:done:<job id>`, then reads `HGET <base>:jobs <job id>` once, for a job that ended before the subscribe, and SHOULD read it again about once a second while it waits, for a message lost on a reconnect. From a record: `state`; `error` = the last `errors` entry's `error`; `result`. A record trimmed before the read and a missed message leave the waiter waiting; it SHOULD have a timeout.

### 6.14 Cancel a job (since 1.7.0)

Cancel is cooperative: storage drops a waiting job, and tells consumers about a running one; a running job stops only if its worker watches for it.

- **Key.** `<base>:cancelled`, ZSET: member a Job ID with a cancel request, score the time the request expires (ms), 24 h after it was made. A member whose score is past is absent. Purge deletes the key.
- **CancelJob.** Input: Job ID. One script: if `HGET <base>:jobs <id>` is a record with `state` `completed`, `failed` or `cancelled`, report "ended" and change nothing. Else `ZREMRANGEBYSCORE <base>:cancelled -inf <now>`, `ZADD <base>:cancelled <now + 86400000> <id>`, `PEXPIRE <base>:cancelled 86400000` (a backstop only), `PUBLISH <base>:control cancel:<id>`; report "requested". Storage cannot tell a waiting job from an unknown ID, so an application that lets untrusted callers cancel by ID MUST authorize and rate-limit them. A workflow item is not dropped at pop (Pop, below): a waiting workflow task or undo is cancelled with Cancel a workflow (6.11); CancelJob still reaches a running one through the message.
- **Pop.** Every pop (one item or a batch, FIFO or priority), after the pause gate: when `EXISTS <base>:cancelled` is 1, for each item it would hand out, read the envelope `id` (`pcall(cjson.decode)`; an unparsable item is handed out). On a storage that implements workflows (sections 6.9 to 6.12), an item whose workflow fields are valid (section 3) and whose `id` is `wf:<wf>:<task>`, or `wf:<wf>:<task>:undo` for `undo` true, is handed out and its mark stays: pop cannot apply the workflow's end (6.10), so dropping it would leave the workflow running for good. The mark does not stop it: it runs unless its workflow is cancelled (6.11, 6.12), so a waiting workflow task is cancelled with the workflow. Any other item, including one with invalid workflow fields or any item on a storage without workflows, is checked as a plain job, so a crafted `wf` field cannot escape a cancel. Else, if `ZSCORE <base>:cancelled <id>` is above now: do not hand it out; `ZREM` it; `ZREM <base>:completed <id>` and `HDEL <base>:attempts <id>`, as a finish does; write a record with `state` `cancelled` (fields as section 4, no `started_at`, `errors` absent), indexed in `<base>:failed` under failed retention; `PUBLISH <base>:done:<id>` with outcome `{"id":<id>,"state":"cancelled","error":"cancelled"}`; count it neither completed nor failed; go on with the next item. One script MUST drop at most 1000 items; at that cap it returns what it has (maybe nothing), reports items left, and does not join `<base>:idle`, so the caller pops again. Retention trims once per script, after the drops. When the set is absent a pop does no more than before.
- **Running job.** A consumer that gets `cancel:<id>` on `<base>:control` SHOULD first check that `ZSCORE <base>:cancelled <id>` is above now (the storage, before it calls its cancel hooks): a finish or a re-add already removed the mark, so a stale message (late, for an earlier run of a reused ID) is dropped. This narrows the window, it does not close it. A consumer that runs job `<id>` SHOULD then stop it (in Go: cancel its context). A message with no consumer listening is lost; the mark still drops the job if it is requeued. A job that then ends because of it is finished with **Acknowledge cancelled**: as Reject (6.4), counted `failed` in `:stats`, but the record `state` is `cancelled`, the outcome `state` is `cancelled` with `error` the reason, never requeued, and `ZREM <base>:cancelled <id>`. A job that ends in another way is finished as usual and also `ZREM`s the id.
- **Re-add.** An Enqueue with a Job ID that pushes (6.13 Re-add) also does `ZREM <base>:cancelled <id>`.
- **Prune.** The once-a-second sweep SHOULD `ZREMRANGEBYSCORE <base>:cancelled -inf <now>`.
- **Outcome.** `cancelled` is a final outcome state (6.13 Watch): a record or message with it ends a watch. From a cancelled record without `errors`, `error` is `cancelled`.
- **Waiting job.** A cancelled waiting job stays in the queue until a consumer pops it; its outcome comes then. A workflow item is the exception (Pop): it runs unless its workflow was cancelled. Listing (7.2): cancelled records show under `failed` with `state` `cancelled`.

## 7. Reading

A reader (in Go, a `QueueInspector`) answers the two list routes as follows. It SHOULD read each answer in one script or `MULTI` transaction, so that the numbers come from one instant.

A reader MUST accept a Job State it does not know, in a Job Record or from the HTTP API, and show the value as it is. It MUST NOT treat it as an error. Since 1.5.0 a task of a workflow can be `waiting` or `cancelled` (section 7.3). Since 1.7.0 any job can be `cancelled` (section 6.14). This rule lets a later version add a state as a minor change.

### 7.1 Counts and totals (`GET /queues`)

| Field | Source |
|---|---|
| `counts.pending` | `LLEN <base>` (fifo) or `ZCARD <base>` (prio) |
| `counts.processing` | `HLEN <base>:nacked` |
| `counts.completed` | `ZCARD <base>:completed` |
| `counts.failed` | `ZCARD <base>:failed` |
| `totals.submitted`, `totals.completed`, `totals.failed` | `HMGET <base>:stats submitted completed failed`, a missing field as 0 |
| `paused`, `paused_at` | Since 1.3.0: `GET <base>:paused`. Absent: `paused` false, no `paused_at`. Present: `paused` true, `paused_at` its value, or no `paused_at` if the value is not an integer. |

`counts.completed` and `counts.failed` are the records retention keeps. The totals count every job since the counters began.

### 7.2 Jobs (`GET /queues/{id}/jobs`)

Page bounds: `start = offset`, `stop = offset + limit - 1`.

- **pending**: `LRANGE <base> start stop`, or `ZRANGE <base> start stop` with the sequence prefix removed. For each item, the Job ID is the envelope `id`; `attempts` is `HGET <base>:attempts <id>`, or 0. No `started_at`.
- **processing**: all fields of `<base>:nacked`, sorted by ack ID, then the page. The Job ID follows section 3.1. `attempts = 1 + (HGET <base>:attempts <id> or 0)`. `started_at` is `HGET <base>:started <ack id>`.
- **completed**, **failed**: `ZREVRANGE <base>:<state> start stop`, then `HMGET <base>:jobs` for those members. Skip a member whose record is gone (a trim ran between the two reads). Since 1.7.0 the `failed` page also holds cancelled records, with `state` `cancelled` (section 6.14).
- **cancelled** (HTTP `state=cancelled`, `openapi.yaml` 1.4.0): this version has no index of its own for cancelled jobs. A reader returns an empty page, or the `failed` records whose `state` is `cancelled`.

`GET /queues/{id}/jobs/{jobId}` (`openapi.yaml` 1.4.0) has no reading rule in this version. A storage that does not answer it returns HTTP 501.

Mapping to the HTTP `Job`: `payload` is the envelope's `data`; `enqueued_at` is the envelope's `enqueued_at`; the other fields come from the record or as above. For an item that is not an envelope, `payload` is the item as a JSON string (or the item itself, if it is valid JSON) and `enqueued_at` is absent.

### 7.3 Workflows (`GET /workflows`)

Since 1.5.0. A reader answers the HTTP routes `GET /workflows`, `GET /workflows/{id}` and `PATCH /workflows/{id}/actions/cancel` (the last is section 6.11).

- **List**: a scan of `wf:index` from the newest entry (`ZREVRANGE`). For each ID, `HGET wf:<id> meta`; skip an ID whose hash is gone (it expired before a trim). `offset` and `limit` count matches: the reader skips the first `offset` workflows that match (every one without a `state` filter, else those in that state) and returns up to `limit` more, reading further entries as needed. Without a `state` filter and with no gone hash, this is `ZREVRANGE wf:index offset offset+limit-1`. An `offset` above 10000 is rejected (HTTP 400), not clamped. `GET /workflows` returns the records mapped below.
- The list MUST NOT run as one script: the reader pages `wf:index` from the client in chunks (a `ZREVRANGE` of a bounded range, then the `meta` reads of that chunk), so one listing never blocks Redis for long. With a `state` filter it reads at most 10000 + `limit` index entries per call (`limit` being the call's; matched or not) and returns what it matched by then. varmq asks every storage for `offset + limit` workflows from offset 0 and merges them, so one HTTP request reads at most that bound per storage.
- **Keys** (optional; Go `WorkflowKeyLister`): the same scan, returning per match only the ID and `created_at` (the `wf:index` score), without the records. varmq merges the keys of every storage first and then reads (as **One**) only the workflows of the final page.
- **One**: `HGETALL wf:<id>`; not found when the hash is absent.
- **Counts** (optional, since 1.9.0; Go `WorkflowCounter`, `GET /workflow-counts`): per state, `ZCOUNT wf:index:states n n` for n = 1 to 7 (section 1.2); every state is returned, 0 included. With the counts, in one `MULTI`, the reader reads `ZCARD wf:index` and `ZCARD wf:index:states`. When they differ it backfills once and counts again: it pages `wf:index` from the client in chunks (as the list; never one script over the whole index) and, per chunk in one script, does `ZADD wf:index:states <n> <id>` for each ID still in `wf:index`, n the state number of its `meta`; when the hash is gone or unreadable the member keeps the score it has and gets 0 only when it has no score (`ZADD wf:index:states NX 0 <id>`), so a workflow whose hash expired keeps counting in its last state until the trim, with or without a backfill in between; then it pages `wf:index:states` the same way and does `ZREM` for each ID that `wf:index` no longer has. A member of `wf:index:states` that is not a valid workflow ID (section 1.2) MUST be skipped (no key is built from it) and SHOULD be removed. Score 0 is no state: counted nowhere, it keeps the two sizes equal until the trim. A reader SHOULD limit the backfill (for example one at a time, and not more often than every 30 seconds per process); when it skips a backfill it returns the counts it read, which can be off until the next one. The counts are exact only when every instance that writes workflows on this Redis runs 1.9.0 (section 9). Like the index, the ZSET keeps a workflow whose hash expired until the trim, so such a workflow still counts (in its last state) while the list already skips it.
- **Trim**: an implementation SHOULD, in its sweep, remove from `wf:index` (and, since 1.9.0, in the same step from `wf:index:states`) the members whose score is at or below `now - retention`, since the hash expires by itself (section 6.10 step 6).

Mapping to `WorkflowRecord`: `id`, `state`, `created_at`, `finished_at` and `undo_on_cancel` from `meta`; per task, in `meta.tasks` order: `name`; `state` from `s:<task>`, with a `waiting` or `cancelled` state shown as it is; `deps`, `always`, `queue_id` (the queue's base key) and `job_id` from `t:<task>`; `reason` from `e:<task>`; `result_size` = the length of `r:<task>`, or absent; `enqueued_at`, `started_at` and `finished_at` (since 1.9.0) from `ea:<task>`, `sa:<task>` and `fa:<task>`, each absent without its field; `undo`, when `t:<task>` has one, from `t:<task>.undo` (queue ID, job ID) and the state, `reason` and (since 1.9.0) `enqueued_at`, `started_at` and `finished_at` in `u:<task>`; an undo with no `u:` shows `waiting`. A reader MUST NOT show the result bytes.

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
- 1.1.0 adds keys and a reader rule only. A 1.0.0 implementation works unchanged beside a 1.1.0 one. An instance on 1.0.0 writes no Instance Record, so a reader that lists instances from section 8 does not see it.
- 1.2.0 adds worker fields and a reporting rule only. A 1.1.0 reader ignores the new fields.
- 1.3.0 adds a key, a channel and a pop rule. A pre-1.3.0 consumer ignores `<base>:paused` and keeps popping, so a queue is paused only once every consumer of it runs 1.3.0. For varmq that means every consumer process runs a varmq with queue pause and a storage on 1.3.0: a varmq without it, on a 1.3.0 storage, keeps asking a paused queue for work and gets none.
- 1.5.0 adds keys and envelope fields, and changes how a workflow task finishes. A pre-1.5.0 consumer does not know `wf`: it settles a workflow task with plain Acknowledge or Reject, so the task ends in its queue but nothing is promoted, the workflow waits for it forever, and the pre-run check (section 6.12) is skipped. Every consumer of a queue that carries workflow tasks MUST run a workflow-aware implementation and a storage on 1.5.0; a producer MUST NOT Submit a workflow unless it has checked that every queue involved is served that way. A job without `wf` is unchanged by 1.5.0. A pre-1.5.0 Purge does not cancel workflow tasks, so it leaves their workflow running. A workflow hash and index entry are not part of any queue, so an older Purge does not touch them.
- 1.6.0 adds a channel and a record field. A pre-1.6.0 consumer publishes no outcome and writes no `result`. A waiter on a job it finishes sees the end only through the record, and gets no result.
- 1.7.0 adds a key, a control message and a pop rule. A pre-1.7.0 consumer ignores `<base>:cancelled`, so it still hands out and runs a cancelled job. A cancel is reliable only once every consumer of the queue runs 1.7.0.
- 1.8.0 changes no data. A consumer process that has no workflow store registered for the scope of a queue holds that queue's workflow items unacked and reports an error. Every consumer process of a workflow queue MUST create the store.
- 1.9.0 adds a key, hash fields and `u:` members, and makes Runnable a script. A pre-1.9.0 reader ignores them. A pre-1.9.0 writer writes neither `wf:index:states` nor the times, and drops the time members of a `u:<task>` it rewrites: its tasks show no (or partial) times. Its new workflows and its trims change the size of one ZSET only, which the next Counts backfill repairs; a state change it makes to a workflow already in `wf:index:states` leaves a stale score that the size check does not see. So the counts are exact only once every writer runs 1.9.0; `DEL wf:index:states` then forces one full backfill.
- A new field in an item, a Job Record or an Instance Record is a minor version. A new Job State is a minor version too (section 7). A changed meaning, or a removed or renamed key or field, is a major version.
