"""Emit varmq-single-worker.svg. Keyframes authored in seconds, converted to % of D."""
import sys

D = 16.0
ITEM_Y, ROW = 246, 266
HEAD, ENTRY = 330, 140


def pct(t):
    return f"{t / D * 100:.2f}".rstrip("0").rstrip(".") + "%"


def kf(name, pts):
    """pts: [(t, {prop: value})], sorted. Pads 0%/100% with first/last props."""
    if pts[0][0] > 0:
        pts = [(0, pts[0][1])] + pts
    if pts[-1][0] < D:
        pts = pts + [(D, pts[-1][1])]
    body = " ".join(
        f"{pct(t)} {{ " + " ".join(f"{k}: {v};" for k, v in p.items()) + " }"
        for t, p in pts
    )
    return f"    @keyframes {name} {{ {body} }}"


def flash(name, spans, peak):
    """spans: [(start, rise, hold_end, fall_end)] -> opacity pulses."""
    pts = [(0, {"opacity": 0})]
    for s, r, h, f in spans:
        pts += [(s, {"opacity": 0}), (r, {"opacity": peak}), (h, {"opacity": peak}), (f, {"opacity": 0})]
    return kf(name, pts)


def tr(x, y):
    return f"translate({x}px, {y}px)"


def dot(t, x, y, op, fill=None):
    p = {"opacity": op, "transform": tr(x, y)}
    if fill:
        p["fill"] = fill
    return (round(t, 3), p)


def trip(t0):
    """queue head -> processNextJob -> ch1 -> Worker 1 (1.6s). Hidden inside boxes."""
    I, J = "var(--item)", "var(--job)"
    return [
        dot(t0, 382, ROW, 0, I), dot(t0 + .08, 390, ROW, 1), dot(t0 + .30, 446, ROW, 1),
        dot(t0 + .38, 456, ROW, 0, I), dot(t0 + .85, 612, ROW, 0, J), dot(t0 + .93, 626, ROW, 1),
        dot(t0 + 1.15, 696, ROW, 1), dot(t0 + 1.22, 704, ROW, 0), dot(t0 + 1.38, 748, ROW, 0),
        dot(t0 + 1.45, 756, ROW, 1), dot(t0 + 1.55, 768, ROW, 1), dot(t0 + 1.6, 776, ROW, 0, J),
    ]


def add_signal(t0):
    """Add() -> event loop (0.9s)."""
    return [dot(t0, 80, 266, 0), dot(t0 + .05, 80, 256, 1), dot(t0 + .35, 80, 44, 1),
            dot(t0 + .85, 444, 44, 1), dot(t0 + .9, 448, 44, 0)]


def done_signal(t0):
    """Worker 1 -> event loop (1.2s)."""
    return [dot(t0, 890, ROW + 30, 0), dot(t0 + .08, 898, ROW + 30, 1), dot(t0 + .15, 935, ROW + 30, 1),
            dot(t0 + .55, 935, 58, 1), dot(t0 + 1.12, 626, 58, 1), dot(t0 + 1.2, 622, 58, 0)]


def item(t_add, t_out):
    o = lambda t, x, op: (t, {"opacity": op, "transform": tr(x, 0)})
    return [o(t_add, ENTRY, 0), o(t_add + .15, ENTRY + 22, 1), o(t_add + .6, HEAD, 1),
            o(t_out, HEAD, 1), o(t_out + .25, HEAD + 24, 0)]


def ring(times):
    pts = [(0, {"opacity": 0, "transform": "scale(1)"})]
    for t in times:
        pts += [(t, {"opacity": 0, "transform": "scale(1)"}), (t + .1, {"opacity": .9, "transform": "scale(1.04)"}),
                (t + .6, {"opacity": 0, "transform": "scale(1.18)"})]
    return kf("ring", pts)


# ---- timeline (seconds) ----
A_ADD, B_ADD = .2, 4.2
A_TRIP, B_TRIP = 2.2, 8.2
A_BUSY, B_BUSY = (3.8, 6.2), (9.8, 12.2)
PULSES = [1.4, 5.4, 7.4, 13.4]

frames = "\n".join([
    ring(PULSES),
    flash("add", [(A_ADD, A_ADD + .1, A_ADD + .25, A_ADD + .5), (B_ADD, B_ADD + .1, B_ADD + .25, B_ADD + .5)], .25),
    flash("yes", [(1.6, 1.75, 1.95, 2.2), (7.6, 7.75, 7.95, 8.2)], .3),
    flash("no", [(5.6, 5.75, 6.1, 6.4), (13.6, 13.75, 14.2, 14.6)], .3),
    flash("busy-why", [(5.6, 5.75, 6.2, 6.5)], 1),
    flash("empty-why", [(13.6, 13.75, 14.5, 14.8)], 1),
    flash("proc", [(A_TRIP + .3, A_TRIP + .4, A_TRIP + .8, A_TRIP + .95), (B_TRIP + .3, B_TRIP + .4, B_TRIP + .8, B_TRIP + .95)], .18),
    flash("busy", [(A_BUSY[0] - .1, A_BUSY[0] + .1, A_BUSY[1], A_BUSY[1] + .3), (B_BUSY[0] - .1, B_BUSY[0] + .1, B_BUSY[1], B_BUSY[1] + .3)], .14),
    kf("bar", [(0, {"opacity": 1, "transform": "scaleX(0)"})]
       + [p for s, e in (A_BUSY, B_BUSY) for p in (
           (s, {"opacity": 1, "transform": "scaleX(0)"}), (e, {"opacity": 1, "transform": "scaleX(1)"}),
           (e + .3, {"opacity": 0, "transform": "scaleX(1)"}), (e + .31, {"opacity": 1, "transform": "scaleX(0)"}))]),
    kf("item-a", item(A_ADD, 2.1)),
    kf("item-b", item(B_ADD, 8.1)),
    kf("job-a", trip(A_TRIP)),
    kf("job-b", trip(B_TRIP)),
    kf("sig-add", [dot(0, 80, 266, 0)] + add_signal(.5) + add_signal(4.5)),
    kf("sig-done", [dot(0, 890, ROW + 30, 0)] + done_signal(6.2) + done_signal(12.2)),
])

tpl = open(sys.argv[1]).read()
import kfu
kfu.D = D
open(sys.argv[2], "w").write(kfu.fill(tpl, still=5.9).replace("/*KEYFRAMES*/", frames.strip()))  # still: reduced-motion frame
