"""Emit varmq-lifecycle.svg: the seven worker states beside a worker that walks through them.

One scenario, matching the lifecycle page: WithAutoRun(false), three jobs added, Start, Pause while
job 1 runs, Resume, Stop while job 2 runs, Restart, job 3 runs. Concurrency 1. Waiting jobs are drawn
as "waiting", which covers both the queue and a batch parked in the worker.
"""
import sys

import kfu

kfu.D = D = 20.0
FADE = .2

NODE = {  # state: (x, y) of a 120x44 box
    "Initiated": (40, 60), "Idle": (240, 60), "Running": (440, 60),
    "Paused": (240, 180), "Pausing": (440, 180),
    "Stopped": (240, 300), "Stopping": (440, 300),
}
MOVING = {"Pausing", "Stopping"}

EDGES = {  # id: path; labels are static text in the template
    "start": "M160 82H238",
    "run": "M360 72H438",
    "done": "M440 94H362",
    "pause-idle": "M318 104V178",
    "resume": "M262 180V106",
    "pause-run": "M500 104V178",
    "paused": "M440 202H362",
    "stop": "M560 82H590V322H562",
    "stop-pausing": "M560 202H590",
    "stopped": "M440 322H362",
    "restart": "M240 322H210V98H238",
}

# ---- timeline (seconds): each list covers [0, D) and ends where it starts ----
STATES = [(0, "Initiated"), (3.0, "Idle"), (3.6, "Running"), (4.8, "Pausing"), (6.6, "Paused"),
          (8.6, "Idle"), (9.1, "Running"), (10.3, "Stopping"), (12.1, "Stopped"),
          (14.0, "Idle"), (14.5, "Running"), (16.5, "Idle"), (19.3, "Initiated")]
CALLS = [(0, "new"), (1.6, "add"), (3.0, "start"), (4.8, "pause"), (8.6, "resume"),
         (10.3, "stop"), (14.0, "restart"), (19.3, "new")]
CALL_TEXT = {
    "new": "NewWorker(fn, WithAutoRun(false))", "add": "queue.Add(1); Add(2); Add(3)",
    "start": "worker.Start()", "pause": "worker.Pause()", "resume": "worker.Resume()",
    "stop": "worker.Stop()", "restart": "worker.Restart()",
}
EXPLAIN = {
    "Initiated": "created, not started yet",
    "Idle": "started, no job running",
    "Running": "a job is running",
    "Pausing": "the running job finishes, no new one starts",
    "Paused": "nothing runs until Resume()",
    "Stopping": "the running job finishes, then goroutines exit",
    "Stopped": "loop ended; waiting jobs stay queued",
}
POOL = {"Initiated": "not started", "Idle": "no job running", "Paused": "no job running",
        "Stopped": "goroutines gone"}
FLASHES = {"start": [3.0], "run": [3.45, 9.0, 14.4], "pause-run": [4.8], "paused": [6.6],
           "resume": [8.6], "stop": [10.3], "stopped": [12.1], "restart": [14.0], "done": [16.5]}
# job: (appear, dispatch, done)
JOBS = {1: (1.8, 3.15, 6.6), 2: (2.1, 8.75, 12.1), 3: (2.4, 14.15, 16.5)}
SLOT_X, SLOT_Y, WORKER_X, LIFT_Y = 660, 257, 857, 222


def segments(timeline, key):
    """Opacity keyframes for element `key`: visible while the timeline's value is key, crossfaded."""
    vals = [v for _, v in timeline]
    vis = [1 if v == key else 0 for v in vals]
    pts = [(0, {"opacity": vis[0]})]
    for (t, _), prev, cur in zip(timeline[1:], vis, vis[1:]):
        if prev != cur:
            pts += [(t, {"opacity": prev}), (t + FADE, {"opacity": cur})]
    assert vis[-1] == vis[0], key
    return pts


def mapped(timeline, table):
    return [(t, table.get(v)) for t, v in timeline]


svg = []
nodes = []
for s, (x, y) in NODE.items():
    cls = "node move" if s in MOVING else "node"
    nodes.append(f'<rect class="{cls}" x="{x}" y="{y}" width="120" height="44" rx="10"/>')
    nodes.append(f'<rect id="hl-{s}" class="a hl {s}{' move' if s in MOVING else ''}" x="{x}" y="{y}" width="120" height="44" rx="10"/>')
    nodes.append(f'<text class="t" x="{x + 60}" y="{y + 22}">{s}</text>')
    kfu.kf(f"hl-{s}", segments(STATES, s))

edges = [f'<path class="edge" d="{d}" marker-end="url(#a)"/>' for k, d in EDGES.items() if k != "stop-pausing"]
edges.append(f'<path class="edge" d="{EDGES["stop-pausing"]}"/>')
flashes = []
for k, times in FLASHES.items():
    flashes.append(f'<path id="f-{k}" class="a fl" d="{EDGES[k]}"/>')
    kfu.flash(f"f-{k}", [(t, t + .5) for t in times], 1)

calls = []
for k, text in CALL_TEXT.items():
    calls.append(f'<text id="c-{k}" class="a call" x="795" y="64">{text}</text>')
    kfu.kf(f"c-{k}", segments(CALLS, k))

pills, status, explain = [], [], []
for s in NODE:
    pills.append(f'<rect id="p-{s}" class="a hl {s}" x="650" y="126" width="290" height="40" rx="8"/>')
    status.append(f'<text id="s-{s}" class="a st" x="795" y="146">"{s}"</text>')
    explain.append(f'<text id="x-{s}" class="a ex" x="795" y="186">{EXPLAIN[s]}</text>')
    for p in ("p", "s", "x"):
        kfu.kf(f"{p}-{s}", segments(STATES, s))

pool = []
pool_tl = mapped(STATES, POOL)
for i, text in enumerate(dict.fromkeys(POOL.values())):
    pool.append(f'<text id="w-{i}" class="a m" x="875" y="275">{text}</text>')
    kfu.kf(f"w-{i}", segments(pool_tl, text))

jobs = []
for n, (appear, go, done) in JOBS.items():
    x = SLOT_X + 44 * (n - 1)
    jobs.append(f'<g id="j{n}" class="a lin"><rect class="item" width="36" height="36" rx="6"/>'
                f'<text class="n" x="18" y="18">{n}</text></g>')
    p = kfu.Path(appear, x - 30, SLOT_Y, 800).to(x, SLOT_Y, op=1)
    p.wait(go - p.t).to(x, LIFT_Y).to(WORKER_X, LIFT_Y).to(WORKER_X, SLOT_Y)
    p.wait(done - p.t).wait(.25, op=0)
    assert p.pts[-1][0] < D
    kfu.kf(f"j{n}", p.pts)

kfu.bar("bar", [(go + .35, done) for _, go, done in JOBS.values()])

s = open(sys.argv[1]).read()
for slot, items in [("EDGES", edges), ("FLASHES", flashes), ("NODES", nodes), ("CALLS", calls),
                    ("PILLS", pills), ("STATUS", status), ("EXPLAIN", explain), ("POOL", pool), ("JOBS", jobs)]:
    s = s.replace(f"<!--{slot}-->", "\n  ".join(items))
css = "\n".join(f"    #{n} {{ animation-name: {n}; }}" for n, _ in kfu.rules)
frames = "\n".join(r for _, r in kfu.rules)
s = kfu.fill(s, still=5.6)  # still: Pausing, job 1 running, jobs 2 and 3 waiting
open(sys.argv[2], "w").write(s.replace("/*KEYFRAMES*/", (css + "\n" + frames).strip()))
