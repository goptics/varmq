# Animated diagram generators

Sources for the animated SVGs in the main README. Do not hand-edit `../varmq-*.svg`; edit here and run:

```bash
make diagrams
```

Needs only Python 3 (stdlib). Output is deterministic, so `git diff` shows exactly what changed.

| Output | Generator | Template | Shows |
|---|---|---|---|
| `varmq-single-worker.svg` | `gen_single.py` | `single-worker.tpl.svg` | event loop, 1 queue, concurrency 1 |
| `varmq-worker-pool.svg` | `gen_pool.py` | `worker-pool.tpl.svg` | `Add` ×6, concurrency 4 |
| `varmq-priority-queue.svg` | `gen_priority.py` | `priority-queue.tpl.svg` | priority queue reordering, concurrency 1 |
| `varmq-multi-queue.svg` | `gen_multi.py` | `multi-queue.tpl.svg` | 3 queues, RoundRobin, concurrency 2 |
| `varmq-topic.svg` | `gen_topic.py` | `topic.tpl.svg` | in-memory topic, 2 subscribers, per-cursor reads, chunk freeing |

## How it works

- Template = static SVG (layout, colours, text). Generator = timeline. It writes `@keyframes` into the template's `/*KEYFRAMES*/` slot.
- Timelines are authored in **seconds**; `kfu.py` converts them to `%` of the loop length `D`. Keyframe name == element `id`.
- `kfu.Path` moves a dot at constant speed along straight segments; dots hide (opacity 0) while inside boxes.
- `gen_multi.py` and `gen_topic.py` derive their timelines from a small simulation of the event loop (worker slots, signals, cursors). `topic.tpl.svg` has a `<!--TOKENS-->` slot for the moving dots the simulation creates.
- `gen_single.py` predates `kfu.py` and keeps its own hand-written timeline.

## Rules (why it is built this way)

- **CSS `@keyframes` only.** GitHub renders README SVGs through `<img>`: no JavaScript, and SMIL / `offset-path` are unreliable. Use the `r` attribute, not CSS `r` (Safari).
- **Seamless loop.** Every animation ends in its start state: empty queues, idle workers.
- **Match varmq's real behaviour.** Queues start empty (the event loop dispatches immediately). `Add` / `AddAll` signal once per job, but the signal channel holds one pending signal, so extra signals are drawn as dropped (`worker_eventloop.go` `notifyToPullNextJobs`). Done workers signal through `releaseProcessingSlot`.
- **Themes.** Colours are CSS variables, swapped by `@media (prefers-color-scheme: dark)`. Text must keep WCAG AA contrast (4.5:1) in both themes, also while a shape is flashing.
- **Reduced motion.** `prefers-reduced-motion` pauses on an informative frame, chosen by the `still=` argument to `kfu.render` / `kfu.fill`.

## Checking a frame

Pause any moment `T` (seconds) and screenshot it in headless Chrome:

```bash
sed "s#</style>#.a{animation-play-state:paused!important;animation-delay:-Ts!important}</style>#" ../varmq-worker-pool.svg > /tmp/frame.svg
google-chrome --headless=new --hide-scrollbars --window-size=960,420 --screenshot=/tmp/frame.png file:///tmp/frame.svg
```
