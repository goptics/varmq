"""varmq-batch.svg: batched dispatch. 1 queue, concurrency 2, Add() called 8 times.

processNextJob hands a worker its fair share of the backlog, pending / min(concurrency, pending),
in one go (worker_eventloop.go batchSize), capped by batchBudget: with one live worker a batch of 4
needs jobs measured under about 10.7 µs (ceil(32 µs / job) >= 4) by an earlier batch, which the seamless
loop implies. First wake-up: 8 / 2 = 4 jobs to Worker 1,
then 4 / 2 = 2 to a spawned Worker 2, both slots busy, 2 jobs wait. A worker runs its buffered jobs
back to back and signals done once per hand-off; the later wake-ups find pending <= workers, so one
job each.
"""
import sys
import kfu
from kfu import Path, at, flash, kf, ring, bar

kfu.D = D = 17.0
Y = 296                       # main row centre
ROWS = [262, 322]             # worker row centres
N, C = 8, 2
STEP, ENTRY, HEAD = 26, 110, 347
SLOTS = [165 + STEP * i for i in range(N)]
PARK = [768 - 19 * i for i in range(4)]   # buffer slots, head nearest the worker; shifts like the queue
I, J = "var(--item)", "var(--job)"

# ---- producer burst + first wake ----
ARR = [.2 + .15 * i for i in range(N)]
add = Path(ARR[0] + .3, 70, 266, 520).to(70, 256, op=1).to(70, 44).to(440, 44).to(446, 44, op=0)
kf("sig-add", add.pts)
for k in range(1, N):   # the 1-slot signal channel already holds the first signal; the rest drop
    t = ARR[k] + .3
    kf(f"drop{k}", [at(t, 70, 266, 0), at(t + .05, 70, 256, 1), at(t + .35, 70, 232, 0)])
flash("drop-why", [(ARR[1] + .3, ARR[N - 1] + .9)], 1)
wake = add.t

# ---- hand-offs: (worker row, jobs, share label); pop = when processNextJob runs ----
# Worker 1's jobs are quick, Worker 2's slower; Worker 1 still finishes last of the two and ends up the idle one.
RUN = {0: 1.0, 1: 1.0, 2: 1.0, 3: 1.0, 4: 1.25, 5: 1.25, 6: 3.5, 7: 1.0}
HANDOFFS = [(0, [0, 1, 2, 3], "k4"), (1, [4, 5], "k2"), (1, [6], "k1"), (0, [7], "k1b")]
pending = N
for _, jobs, _ in HANDOFFS:
    assert len(jobs) == max(1, pending // min(C, pending)), (pending, jobs)   # batchSize(pending, min(C, pending))
    pending -= len(jobs)
assert pending == 0

pop = [wake + .4, wake + .8]          # inner for-loop: 2 back-to-back from one signal, then both busy
DONE, disp, arrive = {}, {}, {}
proc, busy = [], {r: [] for r in range(C)}


def done_signal(k, row):
    cy = ROWS[row]
    p = Path(DONE[k], 900, cy, 470).to(908, cy, op=1).to(934, cy).to(934, 58).to(630, 58).to(624, 58, op=0)
    kf(f"done{k}", p.pts)
    return p.t


def handoff(h):
    row, jobs, _ = HANDOFFS[h]
    cy = ROWS[row]
    paths, parked = [], []
    for i, k in enumerate(jobs):
        disp[k] = pop[h] + .1 * i
        p = Path(disp[k] + .1, 382, Y, 420, I).to(390, Y, op=1).to(446, Y).to(456, Y, op=0, fill=I)
        t_in = p.t
        p.wait(.45).to(612, Y, fill=J)
        proc.append((t_in, p.t))
        p.to(626, Y, op=1).to(650, Y).to(650, cy).to(PARK[i], cy)   # into the channel buffer, visible
        paths.append(p)
        parked.append(p.t)
    leave = []
    for i, k in enumerate(jobs):
        p = paths[i]
        x = PARK[i]
        for j in range(i):              # like the queue: shift one slot toward the worker as the head leaves
            t = max(leave[j] + .1, parked[i] + .02)
            p.pts += [at(t, x, cy, 1), at(t + .3, x + 19, cy, 1)]
            x += 19
        assert x == PARK[0]
        t_go = parked[i] if i == 0 else max(leave[i - 1] + .4, DONE[jobs[i - 1]] + .3)   # the head goes straight through
        p.pts.append(at(t_go, PARK[0], cy, 1))
        p.t, p.x = t_go, PARK[0]
        p.to(782, cy).to(812, cy).to(822, cy, op=0)
        leave.append(t_go)
        kf(f"job{k}", p.pts)
        if i:
            assert p.t >= DONE[jobs[i - 1]] + .32, (k, p.t)   # bar() needs .31s to reset between runs
        busy[row].append((p.t, p.t + RUN[k]))
        DONE[k] = p.t + RUN[k]
    arrive[jobs[-1]] = done_signal(jobs[-1], row)   # one done signal per hand-off, after its last job


handoff(0)
handoff(1)
# Worker 2 (2 jobs) frees first and takes job 6; then Worker 1 frees and takes job 7. Each dispatch must land
# before the other worker frees, or the animation's signal travel time would misrepresent PopBack.
pop.append(arrive[5] + .4)
assert pop[2] < DONE[3], (pop[2], DONE[3])
handoff(2)
pop.append(arrive[3] + .4)
assert pop[3] < DONE[6], (pop[3], DONE[6])
handoff(3)
assert DONE[7] < DONE[6], DONE                  # Worker 1 is already idle in the pool when Worker 2 finishes, so Worker 2 exits
assert [j for _, js, _ in HANDOFFS for j in js] == list(range(N))

# ---- queue items: arrive, shift right as heads leave, exit at head ----
for i in range(N):
    x = SLOTS[N - 1 - i]
    pts = [at(ARR[i], ENTRY, 0, 0), at(ARR[i] + .12, ENTRY + 20, 0, 1), at(ARR[i] + .5, x, 0, 1)]
    for h, (_, jobs, _) in enumerate(HANDOFFS):
        if i in jobs:
            break
        assert ARR[i] + .5 < pop[h] + .1
        pts += [at(pop[h] + .1, x, 0, 1), at(pop[h] + .4, x + STEP * len(jobs), 0, 1)]
        x += STEP * len(jobs)
    assert x == HEAD - STEP * jobs.index(i), (i, x)
    pts += [at(disp[i], x, 0, 1), at(disp[i] + .25, x + 24, 0, 0)]
    kf(f"item{i}", pts)

# ---- event loop, check, labels, glows ----
ring("ring", [wake] + list(arrive.values()))
flash("add-glow", [(.15, 1.4)], .25)
flash("yes", [(t - .3, t - .1) for t in pop], .3, rise=.08, fall=.1)
for h, (_, jobs, label) in enumerate(HANDOFFS):
    flash(label, [(pop[h], disp[jobs[-1]] + .9)], 1)
busy_span = [(pop[1] + .3, busy[1][0][0] + .9)]   # hold until both workers visibly busy
empty = [(arrive[k] + .15, arrive[k] + .8) for k in (6, 7)]
assert all(s > pop[-1] for s, _ in empty)
flash("no", busy_span + empty, .3)
flash("why-busy", busy_span, 1)
flash("why-empty", empty, 1)
flash("proc-glow", proc, .18)

# ---- pool size: mirror processNextJob / freePoolNode (no idle expiry, min idle 1) ----
GHOST = .15
pool, alive, events, left = [0], {0: [(0, None)]}, [], N
events += [(pop[h], "disp", row, len(jobs)) for h, (row, jobs, _) in enumerate(HANDOFFS)]
events += [(DONE[jobs[-1]], "done", row, len(jobs)) for row, jobs, _ in HANDOFFS]
for t, kind, r, n in sorted(events):
    if kind == "disp":
        left -= n
        if pool:
            assert pool.pop() == r, (t, r)      # PopBack hands the batch to the idle worker
        else:
            assert r not in alive or alive[r][-1][1] is not None, (t, r)
            alive.setdefault(r, []).append((t, None))   # spawn
    elif left > len(pool) or len(pool) < 1:     # freePoolNode: backlog outgrows idle workers, or none idle
        pool.append(r)
    else:                                       # another worker already idle -> this one exits
        alive[r][-1] = (alive[r][-1][0], t + .35)
assert pool == [0] and all(e for _, e in alive[1]), (pool, alive)
pts = [(0, {"opacity": GHOST})]
for s, e in alive[1]:
    pts += [(s - .1, {"opacity": GHOST}), (s + .2, {"opacity": 1}), (e, {"opacity": 1}), (e + .4, {"opacity": GHOST})]
kf("w2-row", pts)

for r in range(C):
    flash(f"w{r + 1}-glow", busy[r], .14)
    bar(f"w{r + 1}-bar", busy[r])

assert max(arrive.values()) + 1.2 < D, max(arrive.values())
kfu.render(sys.argv[1], sys.argv[2], still=4.6)  # reduced-motion frame: 4 jobs buffered at Worker 1, 2 at Worker 2
