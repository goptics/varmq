"""Keyframe helpers: author in seconds, emit % of loop D. Keyframe name == element id."""

D = 16.0
rules = []  # (id, keyframes css)


def pct(t):
    return f"{t / D * 100:.2f}".rstrip("0").rstrip(".") + "%"


def kf(name, pts):
    pts = sorted(pts, key=lambda p: p[0])
    for (a, _), (b, _) in zip(pts, pts[1:]):
        assert a <= b, (name, a, b)
    if pts[0][0] > 0:
        pts = [(0, pts[0][1])] + pts
    if pts[-1][0] < D:
        pts = pts + [(D, pts[-1][1])]
    assert pts[-1][0] <= D, (name, pts[-1][0])
    body = " ".join(f"{pct(t)} {{ " + " ".join(f"{k}: {v};" for k, v in p.items()) + " }" for t, p in pts)
    rules.append((name, f"    @keyframes {name} {{ {body} }}"))


def merge(spans, pad=0):
    """Merge (start, end) spans that overlap or sit within pad of each other."""
    out = []
    for s, e in sorted(spans):
        if out and s <= out[-1][1] + pad:
            out[-1] = (out[-1][0], max(out[-1][1], e))
        else:
            out.append((s, e))
    return out


def flash(name, spans, peak, rise=.12, fall=.25):
    pts = [(0, {"opacity": 0})]
    for s, e in merge(spans, rise + fall):
        pts += [(s, {"opacity": 0}), (s + rise, {"opacity": peak}), (e, {"opacity": peak}), (e + fall, {"opacity": 0})]
    kf(name, pts)


def tr(x, y):
    return f"translate({x:g}px, {y:g}px)"


def at(t, x, y, op, fill=None):
    p = {"opacity": op, "transform": tr(x, y)}
    if fill:
        p["fill"] = fill
    return (round(t, 3), p)


class Path:
    """Constant-speed walk along straight segments; hide()/show() toggle visibility."""

    def __init__(self, t, x, y, speed, fill=None):
        self.t, self.x, self.y, self.v = t, x, y, speed
        self.pts = [at(t, x, y, 0, fill)]
        self.op = 0

    def to(self, x, y, op=None, fill=None):
        self.t += (abs(x - self.x) + abs(y - self.y)) / self.v
        self.x, self.y = x, y
        if op is not None:
            self.op = op
        self.pts.append(at(self.t, x, y, self.op, fill))
        return self

    def wait(self, dt, op=None, fill=None):
        self.t += dt
        if op is not None:
            self.op = op
        self.pts.append(at(self.t, self.x, self.y, self.op, fill))
        return self


def ring(name, times, gap=.6):
    """Event loop pulses; pulses closer than gap coalesce (like the 1-slot signal channel)."""
    pts, last = [(0, {"opacity": 0, "transform": "scale(1)"})], -9
    for t in sorted(times):
        if t < last + gap:
            continue
        last = t
        pts += [(t, {"opacity": 0, "transform": "scale(1)"}), (t + .1, {"opacity": .9, "transform": "scale(1.04)"}),
                (t + .6, {"opacity": 0, "transform": "scale(1.18)"})]
    kf(name, pts)


def bar(name, spans):
    pts = [(0, {"opacity": 1, "transform": "scaleX(0)"})]
    for s, e in spans:
        pts += [(s, {"opacity": 1, "transform": "scaleX(0)"}), (e, {"opacity": 1, "transform": "scaleX(1)"}),
                (e + .3, {"opacity": 0, "transform": "scaleX(1)"}), (e + .31, {"opacity": 1, "transform": "scaleX(0)"})]
    kf(name, pts)


LEGEND = """  <g transform="translate(110 88)">
    <rect class="item" x="0" y="-6" width="12" height="12" rx="3"/><text class="lg" x="20" y="0">job waiting in queue</text>
    <circle style="fill: var(--job)" cx="6" cy="20" r="6"/><text class="lg" x="20" y="20">job being dispatched</text>
    <rect style="fill: var(--worker)" x="0" y="37" width="12" height="6" rx="2"/><text class="lg" x="20" y="40">worker busy</text>
    <circle style="fill: var(--signal)" cx="6" cy="60" r="5"/><text class="lg" x="20" y="60">signal to event loop</text>
  </g>"""


def fill(s, still):
    """Fill template placeholders shared by every diagram."""
    return s.replace("{D}", f"{D:g}").replace("{STILL}", f"{still:g}").replace("<!--LEGEND-->", LEGEND.strip())


def render(tpl_path, out_path, still):
    css = "\n".join(f"    #{n} {{ animation-name: {n}; }}" for n, _ in rules)
    frames = "\n".join(r for _, r in rules)
    s = fill(open(tpl_path).read(), still).replace("/*KEYFRAMES*/", (css + "\n" + frames).strip())
    open(out_path, "w").write(s)
