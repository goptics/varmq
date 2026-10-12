"""varmq-multi-queue.svg: 3 queues, concurrency 2, RoundRobin. Timeline comes from a tiny event-loop simulation."""
import heapq
import sys
import kfu
from kfu import Path, at, flash, kf, ring, bar

Y = 296                          # processNextJob row
QY = [216, 296, 376]             # queue row centres
WY = [276, 336]                  # worker row centres
SLOTS, STEP, ENTRY = [144, 178, 212, 246], 34, 100
HEAD = SLOTS[-1]
I, J = "var(--item)", "var(--job)"
SIG_V, TRIP_V, GAP = 640, 520, .6

ARR = {0: [.2, .35, .5], 1: [.6], 2: [.8, .95]}      # item arrival per queue
WORK = [1.6, 3.2, 1.6, 1.6, 1.2, 1.4]                 # work time per job, in dispatch order

# ---- producer signals (Add -> event loop). They land close together and coalesce into one wake. ----
paths = {}                       # name -> keyframe points, emitted once D is known
wakes = []                       # heap of (time, kind)
# only the first reaches the loop: the 1-slot channel already holds it, so later sends hit `default:` and drop
p = Path(1.0, 20, QY[0], SIG_V).to(14, QY[0], op=1).to(10, QY[0]).to(10, 44).to(487, 44).to(493, 44, op=0)
paths["add1"] = p.pts
heapq.heappush(wakes, (p.t, "add"))
for q, t in ((1, 1.05), (2, 1.1)):
    paths[f"add{q + 1}"] = [at(t, 20, QY[q], 0), at(t + .05, 14, QY[q], 1), at(t + .35, 10, QY[q] - 16, 0)]

# ---- simulate the event loop ----
queues = {q: list(range(len(ARR[q]))) for q in range(3)}   # item indexes still queued
rr, free_at = 0, [0.0, 0.0]
jobs, pulses, last_wake = [], [], -9
no_busy, no_empty, yes, pick, skip, qm, proc = [], [], [], [], [], [], []
busy = {0: [], 1: []}                                       # job running on the worker (bar)
claim = {0: [], 1: []}                                      # slot taken: curProcessing++ at dispatch (glow)
# worker goroutines: the pool starts with one idle goroutine (W1); it is a LIFO list (PushNode / PopBack)
pool, alive, pending = [0], {0: [(0, None)], 1: []}, []   # pending: (t, w) finishes not yet applied


def settle(t):
    """Apply worker finishes up to t, like freePoolNode: keep the goroutine idle in the pool while the
    queues still hold >= concurrency jobs or no other worker is idle, otherwise stop it."""
    for ft, w in sorted(p for p in pending if p[0] <= t):
        pending.remove((ft, w))
        if sum(map(len, queues.values())) >= 2 or not pool:
            pool.append(w)
        else:
            alive[w][-1] = (alive[w][-1][0], ft + .35)


departs = {q: [] for q in range(3)}

while wakes:
    t, kind = heapq.heappop(wakes)
    t = max(t, last_wake + GAP)                            # buffered signal waits for the loop
    last_wake = t
    pulses.append(t)
    k = 0
    while any(queues.values()):
        d = t + .4 + .4 * k
        settle(d)
        if sum(j[4] > d for j in jobs) >= 2:               # curProcessing == concurrency
            break
        skipped = []
        while not queues[rr]:                              # RoundRobin skips empty queues
            skipped.append(rr)
            rr = (rr + 1) % 3
        q, rr = rr, (rr + 1) % 3
        item = queues[q].pop(0)
        assert ARR[q][item] + .5 < d
        departs[q].append(d)
        n = len(jobs)

        skip += [(sq, (d - .55, d - .4)) for sq in skipped]
        pick.append((q, (d - .35, d)))
        yes.append((d - .3, d - .1))

        # processNextJob -> sendToNextChannel runs right here in the loop: pop the last idle worker or spawn one.
        # (the dot's trip below is only drawing; the worker is already chosen at dispatch time d)
        if pool:
            w = pool.pop()
        else:
            w = next(r for r in (0, 1) if not alive[r] or alive[r][-1][1] is not None)
            alive[w].append((d, None))
        assert free_at[w] <= d, (n, w)

        cy = QY[q]
        p = Path(d + .05, 288, cy, TRIP_V, I).to(296, cy, op=1).to(310, cy).to(310, Y).to(322, Y).to(330, Y, op=0)
        qm.append((p.t, p.t + .3))
        p.wait(.15).to(460, Y).to(468, Y, op=1).to(490, Y).to(500, Y, op=0)
        t_in = p.t
        p.wait(.3).to(660, Y, fill=J)
        proc.append((t_in, p.t))
        ry = WY[w]
        p.to(668, Y, op=1).to(690, Y).to(690, ry).to(712, ry).to(720, ry, op=0).to(770, ry).to(778, ry, op=1).to(786, ry).to(794, ry, op=0)
        paths[f"job{n}"] = p.pts
        done = p.t + WORK[n]
        free_at[w] = done
        pending.append((done, w))
        busy[w].append((p.t, done))
        claim[w].append((d, done))
        jobs.append((q, w, d, p.t, done))

        sig = Path(done, 900, ry, SIG_V).to(908, ry, op=1).to(940, ry).to(940, 58).to(673, 58).to(667, 58, op=0)
        paths[f"done{n}"] = sig.pts
        heapq.heappush(wakes, (sig.t, "done"))
        k += 1

    end = t + .4 + .4 * k
    if any(queues.values()):                               # stopped because every slot is taken
        no_busy.append((end - .1, max(j[3] for j in jobs) + .6))
    elif k == 0:                                           # woke up, nothing left to do
        no_empty.append((t + .15, t + .8))

kfu.D = D = round(max(pulses) + 1.6, 1)
settle(D)
assert pool == [0] and alive[1] and all(e for _, e in alive[1]), (pool, alive)   # loop ends as it starts: only W1
# red "busy" ends when a worker frees its slot or the next signal wakes the loop, whichever is first
no_busy = [(a, min(b, min((p for p in pulses if p > a), default=b) - .15, min(j[4] for j in jobs if j[4] > a))) for a, b in no_busy]
for name, pts in paths.items():
    kf(name, pts)

# ---- queue items ----
for q in range(3):
    for i, a in enumerate(ARR[q]):
        x = SLOTS[3 - i]
        pts = [at(a, ENTRY, 0, 0), at(a + .12, ENTRY + 20, 0, 1), at(a + .5, x, 0, 1)]
        for prev in departs[q][:i]:
            pts += [at(prev + .1, x, 0, 1), at(prev + .4, x + STEP, 0, 1)]
            x += STEP
        assert x == HEAD, (q, i, x)
        dep = departs[q][i]
        pts += [at(dep, x, 0, 1), at(dep + .25, x + 24, 0, 0)]
        kf(f"q{q + 1}i{i}", pts)

# ---- glows ----
ring("ring", pulses)
flash("add-glow", [(.15, 1.0)], .25)
flash("yes", yes, .3, rise=.08, fall=.1)
flash("no", no_busy + no_empty, .3)
flash("why-busy", no_busy, 1)
flash("why-empty", no_empty, 1)
flash("qm-glow", [s for _, s in pick] + qm, .18)
flash("proc-glow", proc, .18)
for q in range(3):
    flash(f"q{q + 1}-pick", [s for qq, s in pick if qq == q], .35, rise=.08, fall=.15)
    flash(f"q{q + 1}-skip", [s for qq, s in skip if qq == q], .35, rise=.05, fall=.1)
GHOST = .15                                                # slot within the concurrency limit, no goroutine
for w in range(2):
    pts = [(0, {"opacity": 1 if w == 0 else GHOST})]
    for s, e in alive[w]:
        if s > 0:
            pts += [(s - .1, {"opacity": GHOST}), (s + .2, {"opacity": 1})]
        if e:
            pts += [(e, {"opacity": 1}), (e + .4, {"opacity": GHOST})]
    kf(f"w{w + 1}-row", pts)
for w in range(2):
    flash(f"w{w + 1}-glow", claim[w], .14)
    bar(f"w{w + 1}-bar", busy[w])

if __name__ == "__main__":
    for n, (q, w, d, s, e) in enumerate(jobs):
        print(f"job{n}: Q{q + 1} -> W{w + 1} dispatch {d:.2f} work {s:.2f}-{e:.2f}")
    print("alive", alive)
    print("pulses", [round(p, 2) for p in pulses], "skips", [(s + 1, round(a, 2)) for s, (a, _) in skip])
    print("no_busy", [(round(a, 2), round(b, 2)) for a, b in no_busy], "no_empty", [(round(a, 2), round(b, 2)) for a, b in no_empty], "D", D)
    kfu.render(sys.argv[1], sys.argv[2], still=4.6)  # reduced-motion frame
