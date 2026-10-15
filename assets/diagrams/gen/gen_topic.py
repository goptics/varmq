"""varmq-topic.svg: one in-memory topic, two subscribed workers, four Publish calls.
Timeline comes from a tiny simulation of each worker's event loop reading its own cursor."""
import heapq
import sys
import kfu
from kfu import Path, at, flash, kf, ring, bar

CX = [150, 264, 378]               # chunk x; two slots per chunk in this drawing (the real first chunk holds 1024)
PAN = CX[0] - CX[2]                # at the end the last chunk slides into the first chunk's place: seamless loop
IY, DIM = 237, .35                 # item row top; opacity of a payload every cursor has read
CY = {"e": 214, "s": 280}          # cursor rows (email above the log, stock below)
I, J = "var(--item)", "var(--job)"
SIG_V, JOB_V, GAP = 1300, 760, .6

PUB = [.4, 1.0, 1.6, 2.2]          # four Publish calls
SLOT = [(1, 0), (1, 1), (2, 0), (2, 1)]   # chunk, index of each payload; chunk 0 holds the previous loop's payloads
WORKERS = {                        # concurrency, work time, rows
    "e": (1, 1.8, [166]),
    "s": (2, .8, [322, 366]),
}


def slot_x(c, i):
    return CX[c] + 10 + 34 * i


def cur_x(c, i):                   # i == 2: the cursor sits at the end of chunk c until its next Dequeue
    return CX[c] + 8 + 34 * i


tokens = []
paths = {}

# ---- Publish: append once, then wake every subscriber ----
avail = []
for k, p in enumerate(PUB):
    c, i = SLOT[k]
    x = slot_x(c, i)
    item = Path(p, 120, 0, JOB_V * 1.4).wait(.05, op=1).to(x, 0)
    avail.append(item.t)
    paths[f"i{k}"] = item.pts                       # completed below: dimmed, freed or panned
    for w in "es":
        sig = Path(p + .05, 44, 228 if w == "e" else 276, SIG_V).to(44, 222 if w == "e" else 282, op=1)
        sig.to(44, 106 if w == "e" else 418).to(640, 106 if w == "e" else 418).to(640, 146 if w == "e" else 364, op=0)
        paths[f"sig{w}{k}"] = sig.pts
        tokens.append(f'<circle id="sig{w}{k}" class="a lin sigdot" r="5"/>')

# ---- each worker's event loop: wake on a signal, dispatch while its cursor has unread payloads and a slot is free ----
reads = {}                          # (worker, payload) -> dispatch time
busy = {}
pulses = {}
last_done = 0
for w, (conc, work, rows) in WORKERS.items():
    wakes = [(paths[f"sig{w}{k}"][-1][0], "pub") for k in range(len(PUB))]
    heapq.heapify(wakes)
    nxt, last, running = 0, -9, []                  # running: (done, row)
    busy.update({(w, r): [] for r in range(len(rows))})
    pulses[w] = []
    while wakes:
        t, _ = heapq.heappop(wakes)
        t = max(t, last + GAP)                      # a buffered signal waits for the loop
        last = t
        pulses[w].append(t)
        n = 0
        while nxt < len(PUB):
            d = t + .3 + .35 * n
            running = [r for r in running if r[0] > d]
            if len(running) >= conc or avail[nxt] > d:   # all slots busy, or nothing unread yet
                break
            row = min(set(range(len(rows))) - {r for _, r in running})
            c, i = SLOT[nxt]
            reads[(w, nxt)] = d
            ry = rows[row]
            if w == "e":
                j = Path(d, slot_x(c, i) + 15, IY, JOB_V, J).to(slot_x(c, i) + 15, IY - 4, op=1).to(slot_x(c, i) + 15, 208).to(520, 208).to(520, 166).to(566, 166, op=0)
            else:
                j = Path(d, slot_x(c, i) + 15, IY + 30, JOB_V, J).to(slot_x(c, i) + 15, IY + 34, op=1).to(slot_x(c, i) + 15, 296).to(520, 296).to(520, 344).to(566, 344, op=0)
            j.to(666, j.y).to(672, j.y, op=1).to(684, j.y).to(684, ry).to(702, ry).to(712, ry, op=0).to(762, ry).to(770, ry, op=1).to(778, ry).to(786, ry, op=0)
            paths[f"job{w}{nxt}"] = j.pts
            tokens.append(f'<circle id="job{w}{nxt}" class="a lin jobdot" r="7"/>')
            done = j.t + work
            busy[(w, row)].append((j.t, done))
            running.append((done, row))
            heapq.heappush(wakes, (done + .25, "done"))   # releaseProcessingSlot signals the loop
            last_done = max(last_done, done)
            nxt += 1
            n += 1
    assert nxt == len(PUB), (w, nxt)

# ---- cursors: step past each payload as it is dequeued ----
for w in WORKERS:
    x, pts = cur_x(0, 2), [at(0, cur_x(0, 2), CY[w], 1)]
    for k in range(len(PUB)):
        c, i = SLOT[k]
        d = reads[(w, k)]
        pts += [at(d, x, CY[w], 1), at(d + .3, cur_x(c, i + 1), CY[w], 1)]
        x = cur_x(c, i + 1)
    paths[f"cur{w}"] = pts

# a chunk is freed once every cursor has dequeued from the next one
enter = {(w, c): min(reads[(w, k)] for k in range(len(PUB)) if SLOT[k][0] == c) for w in WORKERS for c in (1, 2)}
free = {c: max(enter[(w, c + 1)] for w in WORKERS) + .3 for c in (0, 1)}
assert enter[("s", 2)] + 1 < free[1], "stock cursor must run ahead so the kept chunk is visible"
read_all = {k: max(reads[(w, k)] for w in WORKERS) + .3 for k in range(len(PUB))}

END = round(last_done + .8, 2)                       # every worker idle, then pan
PE = END + .9
kfu.D = D = round(PE + .5, 1)

# ---- finish payload paths: dim once read by all; freed with their chunk, or panned with the last chunk ----
for k in range(len(PUB)):
    c, i = SLOT[k]
    x = slot_x(c, i)
    pts = paths[f"i{k}"] + [at(read_all[k], x, 0, 1), at(read_all[k] + .3, x, 0, DIM)]
    if c == 1:
        pts += [at(free[1], x, 0, DIM), at(free[1] + .5, x, 0, 0)]
    else:
        pts += [at(END, x, 0, DIM), at(PE, x + PAN, 0, DIM)]
    kf(f"i{k}", pts)
for i in range(2):
    x = slot_x(0, i)
    kf(f"o{i}", [at(0, x, 0, DIM), at(free[0], x, 0, DIM), at(free[0] + .5, x, 0, 0)])
for w in WORKERS:
    x = cur_x(2, 2)
    kf(f"cur{w}", paths[f"cur{w}"] + [at(END, x, CY[w], 1), at(PE, x + PAN, CY[w], 1)])

for name, pts in paths.items():
    if name.startswith(("sig", "job")):
        kf(name, pts)

# ---- chunks and links ----
op = lambda t, o, x=0: (t, {"opacity": o, "transform": f"translate({x}px, 0px)"})
kf("c0", [op(0, 1), op(free[0], 1), op(free[0] + .5, 0)])
kf("l1", [op(0, 0), op(PUB[0], 0), op(PUB[0] + .3, 1), op(free[0], 1), op(free[0] + .5, 0)])
kf("c1", [op(0, 0), op(PUB[0], 0), op(PUB[0] + .3, 1), op(free[1], 1), op(free[1] + .5, 0)])
kf("l2", [op(0, 0), op(PUB[2], 0), op(PUB[2] + .3, 1), op(free[1], 1), op(free[1] + .5, 0)])
kf("c2", [op(0, 0), op(PUB[2], 0), op(PUB[2] + .3, 1), op(END, 1), op(PE, 1, PAN)])
flash("keep", [(enter[("s", 2)] + .3, free[1] - .1)], 1)
flash("free0", [(free[0], free[0] + 1.2)], 1)
flash("free1", [(free[1], free[1] + 1.2)], 1)

# ---- glows ----
flash("pub-glow", [(p - .1, p + .3) for p in PUB], .25)
ring("ring1", pulses["e"])
ring("ring2", pulses["s"])
for (w, r), spans in busy.items():
    name = f"{w}{r}"
    flash(f"{name}-glow", spans, .14)
    bar(f"{name}-bar", spans)

if __name__ == "__main__":
    for w in WORKERS:
        print(w, "reads", [round(reads[(w, k)], 2) for k in range(len(PUB))], "pulses", [round(p, 2) for p in pulses[w]])
    print("free", {c: round(t, 2) for c, t in free.items()}, "END", END, "D", D)
    css = "\n".join(f"    #{n} {{ animation-name: {n}; }}" for n, _ in kfu.rules)
    frames = "\n".join(r for _, r in kfu.rules)
    s = kfu.fill(open(sys.argv[1]).read(), still=4.4)
    s = s.replace("/*KEYFRAMES*/", (css + "\n" + frames).strip()).replace("<!--TOKENS-->", "\n  ".join(tokens))
    open(sys.argv[2], "w").write(s)
