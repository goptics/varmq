"""varmq-worker-pool.svg: 1 queue, concurrency 4, Add() called 6 times."""
import sys
import kfu
from kfu import Path, at, flash, kf, ring, bar

kfu.D = D = 16.0
Y = 296                       # main row centre
ROWS = [221, 271, 321, 371]   # worker row centres
SLOTS = [162, 196, 230, 264, 298, 332]
STEP, ENTRY, HEAD = 34, 110, 332
I, J = "var(--item)", "var(--job)"

# ---- producer burst + first wake ----
ARR = [.2 + .15 * i for i in range(6)]
add = Path(ARR[0] + .3, 70, 266, 520).to(70, 256, op=1).to(70, 44).to(440, 44).to(446, 44, op=0)
kf("sig-add", add.pts)
# every Add() calls notifyToPullNextJobs(); the 1-slot channel already holds the first signal, so the rest hit `default:` and drop
for k in range(1, 6):
    t = ARR[k] + .3
    kf(f"drop{k}", [at(t, 70, 266, 0), at(t + .05, 70, 256, 1), at(t + .35, 70, 232, 0)])
flash("drop-why", [(ARR[1] + .3, ARR[5] + .9)], 1)
wake = add.t

# ---- dispatch schedule ----
JOB_ROW = [0, 1, 2, 3, 1, 3]
DONE = {0: 12.2, 1: 5.8, 2: 9.6, 3: 8.9, 4: 11.4, 5: 13.3}   # tuned so Worker 1 is the one left idle: loop ends as it starts
disp = [wake + .4 + .4 * k for k in range(4)]   # inner for-loop: 4 back-to-back from one signal


def done_signal(k):
    cy = ROWS[JOB_ROW[k]]
    p = Path(DONE[k], 890, cy, 470).to(898, cy, op=1).to(935, cy).to(935, 58).to(630, 58).to(624, 58, op=0)
    kf(f"done{k}", p.pts)
    return p.t


arrive = {k: done_signal(k) for k in range(6)}
disp.append(arrive[1] + .4)   # Worker 2 frees first -> job 4
disp.append(arrive[3] + .4)   # Worker 4 frees next  -> job 5
assert arrive[1] < arrive[3] < min(arrive[0], arrive[2]), arrive  # those two signals are the ones that find a waiting job

# ---- jobs: queue head -> processNextJob -> chN -> Worker N ----
proc, busy, leave = [], {r: [] for r in range(4)}, {}
for k in range(6):
    cy = ROWS[JOB_ROW[k]]
    p = Path(disp[k] + .1, 372, Y, 420, I).to(380, Y, op=1).to(446, Y).to(456, Y, op=0, fill=I)
    t_in = p.t
    p.wait(.45).to(612, Y, fill=J)
    leave[k] = p.t                              # sendToNextChannel: pop an idle worker or spawn one
    proc.append((t_in, p.t))
    p.to(626, Y, op=1).to(660, Y).to(660, cy).to(694, cy).to(704, cy, op=0).to(748, cy).to(756, cy, op=1).to(768, cy).to(776, cy, op=0)
    kf(f"job{k}", p.pts)
    assert p.t < DONE[k], (k, p.t)
    busy[JOB_ROW[k]].append((p.t, DONE[k]))

# ---- queue items: arrive, shift right as heads leave, exit at head ----
for i in range(6):
    x = SLOTS[5 - i]
    pts = [at(ARR[i], ENTRY, 0, 0), at(ARR[i] + .12, ENTRY + 20, 0, 1), at(ARR[i] + .5, x, 0, 1)]
    for k in range(i):
        assert ARR[i] + .5 < disp[k] + .1
        pts += [at(disp[k] + .1, x, 0, 1), at(disp[k] + .4, x + STEP, 0, 1)]
        x += STEP
    assert x == HEAD, (i, x)
    pts += [at(disp[i], x, 0, 1), at(disp[i] + .25, x + 24, 0, 0)]
    kf(f"item{i}", pts)

# ---- event loop, check, glows ----
ring("ring", [wake] + list(arrive.values()))
flash("add-glow", [(.15, 1.1)], .25)
flash("yes", [(t - .3, t - .1) for t in disp], .3, rise=.08, fall=.1)
busy_span = [(disp[3] + .3, busy[3][0][0] + .9)]  # hold until all 4 workers visibly busy
empty = [(arrive[k] + .15, arrive[k] + .8) for k in (0, 2, 4, 5)]
assert all(s > disp[5] for s, _ in empty)
flash("no", busy_span + empty, .3)
flash("why-busy", busy_span, 1)
flash("why-empty", empty, 1)
flash("proc-glow", proc, .18)
# ---- pool size: mirror sendToNextChannel / freePoolNode (no idle expiry, min idle 1) ----
GHOST = .15                                     # slot within the concurrency limit, no goroutine
pool, alive, events = [0], {0: [(0, None)]}, []
events += [(leave[k], "disp", k) for k in range(6)] + [(DONE[k], "done", k) for k in range(6)]
for t, kind, k in sorted(events):
    r = JOB_ROW[k]
    if kind == "disp":
        if pool:
            assert pool.pop() == r, (k, r)      # PopBack hands the job to the idle worker
        else:
            assert r not in alive or alive[r][-1][1] is not None, (k, r)
            alive.setdefault(r, []).append((t, None))   # spawn
    elif pool:                                  # another worker already idle -> this one exits
        alive[r][-1] = (alive[r][-1][0], t + .35)
    else:
        pool.append(r)                          # stay idle in the pool
assert pool == [0] and all(e for r in (1, 2, 3) for _, e in alive[r]), (pool, alive)
for r in (1, 2, 3):
    pts = [(0, {"opacity": GHOST})]
    for s, e in alive[r]:
        pts += [(s - .1, {"opacity": GHOST}), (s + .2, {"opacity": 1}), (e, {"opacity": 1}), (e + .4, {"opacity": GHOST})]
    kf(f"w{r + 1}-row", pts)

for r in range(4):
    flash(f"w{r + 1}-glow", busy[r], .14)
    bar(f"w{r + 1}-bar", busy[r])

assert max(arrive.values()) + 1.2 < D
kfu.render(sys.argv[1], sys.argv[2], still=5.4)  # reduced-motion frame
