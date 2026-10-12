# VarMQ reel: voice-over script (60s)

Timings match `index.html` (`SCENES`). About 2.5 words per second keeps the pace natural.

| Time | Scene | Say |
|---|---|---|
| 0:00–0:04 | Intro | "VarMQ: job queues and a worker pool for Go. Basic to advanced in sixty seconds." |
| 0:04–0:16 | 1 · Basic usage | "Create a worker with your job function and bind a queue. Every `Add` wakes a single event loop. If a worker is free, the job goes straight to it; if not, it waits its turn." |
| 0:16–0:30 | 2 · Concurrency | "Pass a concurrency, say four. That's a limit, not a pool size: workers spawn only when jobs are waiting, and retire one by one as the queue drains. One wake-up is enough to fill every free slot." |
| 0:30–0:42 | 3 · Priority queue | "Need ordering? Bind a priority queue. Lower number wins, so C, added last with priority zero, jumps ahead of B." |
| 0:42–0:56 | 4 · Multiple queues | "Bind several queues to one worker. The queue manager picks the next job by strategy. Here RoundRobin takes turns and skips empty queues, all on the same pool." |
| 0:56–1:00 | Outro | "go get github.com/goptics/varmq, and drop a star if it helps." |

## Make the video

```bash
make reel
```

Writes `assets/reel/reel.mp4` (1080×1920, 30 fps, silent) and `reel.srt` (captions). Add your voice or face-cam on top in any editor.

Live presenter mode, to screen-record while you talk:

```bash
cd assets/reel && npm run serve
```

Open the printed URL. ←/→ jumps scenes, space pauses.

Change scenes, code or captions in the `SCENES` array in `index.html`. `offset` picks where in its loop each diagram starts. Check single frames with `node render.mjs --at 6,20,35`, which writes `still-<t>.png`.
