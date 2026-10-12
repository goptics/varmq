"""varmq-priority-queue.svg: priority queue, concurrency 1. C (p=0) overtakes B (p=1) while Worker 1 is busy."""
import sys
import kfu
from kfu import at, flash, kf, ring, bar

kfu.D = D = 19.2
ROW, ENTRY, HEAD, STEP = 266, 140, 330, 46
I, J = "var(--item)", "var(--job)"


def trip(t0):
    """queue head -> processNextJob -> ch1 -> Worker 1 (1.6s). Hidden inside boxes."""
    return [at(t0, 382, ROW, 0, I), at(t0 + .08, 390, ROW, 1), at(t0 + .30, 446, ROW, 1),
            at(t0 + .38, 456, ROW, 0, I), at(t0 + .85, 612, ROW, 0, J), at(t0 + .93, 626, ROW, 1),
            at(t0 + 1.15, 696, ROW, 1), at(t0 + 1.22, 704, ROW, 0), at(t0 + 1.38, 748, ROW, 0),
            at(t0 + 1.45, 756, ROW, 1), at(t0 + 1.55, 768, ROW, 1), at(t0 + 1.6, 776, ROW, 0, J)]


def add_signal(t0):
    """Add() -> event loop (0.9s)."""
    return [at(t0, 80, 266, 0), at(t0 + .05, 80, 256, 1), at(t0 + .35, 80, 44, 1), at(t0 + .85, 444, 44, 1), at(t0 + .9, 448, 44, 0)]


def done_signal(t0):
    """Worker 1 -> event loop (1.2s)."""
    return [at(t0, 890, 296, 0), at(t0 + .08, 898, 296, 1), at(t0 + .15, 935, 296, 1),
            at(t0 + .55, 935, 58, 1), at(t0 + 1.12, 626, 58, 1), at(t0 + 1.2, 622, 58, 0)]


# ---- timeline (seconds) ----
ADD = {"a": .2, "b": 2.8, "c": 3.3}
SWAP = 4.0                                   # heap reorders: C (p=0) passes B (p=1)
WORK = 1.6
disp = {"a": 1.6}                            # 'yes' starts; dequeue +.5, trip +.6
order = ["a", "c", "b"]

busy, pulses, yes, empty = [], [1.4], [], []
for k, nxt in zip(order, order[1:] + [None]):
    t_trip = disp[k] + .6
    start = t_trip + 1.6
    end = start + (2.2 if k == "a" else WORK)  # A runs long enough for B and C to queue up behind it
    busy.append((start, end))
    kf(f"job-{k}", trip(t_trip))
    arrive = end + 1.2
    pulses.append(arrive)
    if nxt:
        disp[nxt] = arrive + .2
    else:
        empty.append((arrive + .2, arrive + .9))
    yes.append((disp[k], disp[k] + .4))
done = [s[1] for s in busy]
assert empty[0][1] + .4 < D, empty

# ---- queue items ----
o = lambda t, x, y=0, op=1: at(t, x, y, op)
land = lambda k, x: [o(ADD[k], ENTRY, 0, 0), o(ADD[k] + .15, ENTRY + 22), o(ADD[k] + .6, x)]
leave = lambda t: [o(t, HEAD), o(t + .25, HEAD + 24, 0, 0)]
kf("item-a", land("a", HEAD) + leave(disp["a"] + .5))
kf("item-b", land("b", HEAD) + [o(SWAP, HEAD), o(SWAP + .2, HEAD - STEP / 2, 8), o(SWAP + .4, HEAD - STEP)]
   + [o(disp["c"] + .5, HEAD - STEP), o(disp["c"] + .8, HEAD)] + leave(disp["b"] + .5))
kf("item-c", land("c", HEAD - STEP) + [o(SWAP, HEAD - STEP), o(SWAP + .2, HEAD - STEP / 2, -8), o(SWAP + .4, HEAD)]
   + leave(disp["c"] + .5))
assert ADD["c"] + .6 < SWAP and SWAP + .4 < disp["c"]

# ---- signals: each Add() notifies; the 1-slot channel still holds B's when C's is sent ----
sig = {k: ADD[k] + .3 for k in ADD}
kf("add-a", add_signal(sig["a"]))
kf("add-b", add_signal(sig["b"]))
kf("add-c", [at(sig["c"], 80, 266, 0), at(sig["c"] + .05, 80, 256, 1), at(sig["c"] + .35, 80, 232, 0)])  # B's signal still pending -> dropped
flash("drop-why", [(sig["c"], sig["c"] + .6)], 1)
pulses += [sig["b"] + .9]
kf("sig-done", [p for t in done for p in done_signal(t)])
no_busy = [(sig["b"] + 1.1, sig["b"] + 1.9)]
assert no_busy[0][1] < done[0]

ring("ring", pulses)
flash("add-glow", [(t - .05, t + .25) for t in ADD.values()], .25)
flash("yes", yes, .3, rise=.1, fall=.15)
flash("no", no_busy + empty, .3)
flash("busy-why", no_busy, 1)
flash("empty-why", empty, 1)
flash("proc-glow", [(disp[k] + .9, disp[k] + 1.4) for k in order], .18)
flash("w1-glow", busy, .14)
bar("w1-bar", busy)

if __name__ == "__main__":
    print("disp", disp, "busy", busy, "pulses", pulses)
    kfu.render(sys.argv[1], sys.argv[2], still=4.9)  # reduced-motion frame: C ahead of B, slot busy
