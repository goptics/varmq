# Review of PR #148, and the new docs site built on it

[PR #148](https://github.com/goptics/varmq/pull/148) (draft) redesigns the docs site. It adds:

- interactive scenes to 27 pages;
- a native Benchmarks page;
- a second landing page at `/alt/`.

This file reviews it, and records what the branch `docs/new-site` changes on top of it. That branch keeps all of #148's commits.

## What `docs/new-site` adds

- **Theme switch.** Light, Dark or System, in the header (`components/ui/ThemeSwitch.astro`). In #148 the theme followed the OS, and there was no switch.
  - The choice is stored under Starlight's own key, so it carries between the docs pages and `/alt/`.
  - The tokens switch on `data-theme`, which is set before the first paint.
  - The home page has no sidebar menu, so on a phone its header shows the switch.
- **The home page opens on varmq's story.** It replaces the old hero map.
  - First an intro: the h1, the lede, the install box, Get started and GitHub, beside the whole map. The map runs as in the last step, holds a moment, and runs again until the reader scrolls on.
  - Then ten steps, one per scroll. Each step plays its chapter on the map, while one code block morphs into the code behind it and lights the lines each caption is about.
  - The page snaps only while the reader is in the story. Below it the page scrolls normally.
  - The rest of the home page shares the story's frame, so their edges line up. Each section is a row with its heading and lead in the story's text column and the rest in the map's:
    - Try it: five runnable programs in tabs. The old "Try it" and "A closer look" sections are merged.
    - Where jobs live: a lead, then the three stores as link cards led by their icons (a memory stick, and Redis's and SQLite's logos).
    - Performance.
    - Next.
  - "What you get" is removed: its six cards repeated what the story now shows.
- **Each theme's green is its logo's.** #148's light theme used the dark logo's mint (`#44bf9e`) for buttons and a darker, yellower green for text, beside a light logo in `#2e9c7f`. Now `--brand` is `#2e9c7f` in light and `#44bf9e` in dark, and the light text green is the same hue as the logo. Dark panels keep the dark green in both themes.
- **A light palette for the map.** In dark mode the map keeps its original palette. In light mode it uses the same hues, darkened to read on `--paper`:
  - Text and the other hues' strokes sit at one OKLCH lightness, 0.50 to 0.56, as the page's own on-paper colours do. Each has at least 4.5:1 against the paper.
  - The green of workers and goroutines is the light logo's, as the dark map's is the dark logo's.
  - Fills are the same hues near lightness 0.96.
  - The greys come from the page's green-tinted inks.
  - The reasoning is in `DESIGN.md`, "The map's two palettes".
- **Fixes** for the review's blockers and majors, and for most minors (below).
- **`DESIGN.md`** records the theme switch, the new shared pieces (`.btn`, the install box, the theme switch), the home page story and the map's palettes.

The story was first built as a separate Vite page (`site/`, branch `docs/scroll-story`). On this branch it lives in the docs app (`components/Story.astro`, `lib/story/`, `styles/story.css`). It uses #148's type and tokens, so the site has one type system.

## How #148 was reviewed

- **Three read-only reviews.**
  - Shell and landing page.
  - Learn and guide scenes.
  - Ops, benchmarks and reference pages.
- **What they checked against.**
  - The Go source on `origin/main`, redisq and sqliteq.
  - The raw benchmark files at goptics/benchmarks `a0d0e0b`.
- **What was run.**
  - The real REST handler and the Prometheus example.
  - The Go blocks of the ops and reference pages, built and vetted.
  - `bench-data.mjs --check` and an independent recount of the scoreboard.
- **In the browser.**
  - Pages at 320 to 1440 px wide, in both themes.
  - The home page at 18 viewport sizes, from a 320 px phone to a 1608 px window. Its wheel and keyboard input was tested inside the page.

## Findings

Severity: a **blocker** shows behaviour the library cannot produce, code that does not compile, or a page that cannot be read.

### Blockers

| # | Where | What was wrong | Status |
|---|---|---|---|
| 1 | Every doc code block | Each line was drawn as its own bordered box, and in light mode the code could not be read. `ui.css`'s code card rule `.code` also matched Expressive Code's line elements, which are named `.code` too. The original #148 build shows it. | Fixed: the card rule skips those lines |
| 2 | Lifecycle scene | Jobs 2 and 3 stayed in the queue and the worker went Idle between jobs. At concurrency 1 the library takes the whole backlog (up to 32) in one batch and stays Running. | Fixed: batch and parked strips; the worker stays Running through the batch |
| 3 | Topic scene | Each worker read one payload at a time. The library reads every unread payload into one batch, so a Pause parks them on the worker. | Fixed |
| 4 | Topic fan-out scene | A busy worker read new payloads at once, so Publish under a capacity returned true where the library returns false. | Fixed: only a free worker reads, with tests |
| 5 | Job ID dedup scene | "Worker takes a job" took one job and kept the other IDs held. The library takes every queued job into one batch and frees all their IDs. | Fixed: "Start worker" takes the queue as a batch, with tests |
| 6 | Job ID dedup scene | A refused AddAll item showed `nil, false`. AddAll returns a group handle and has no bool. | Fixed: it shows `NumPending() = 0` |
| 7 | Adapter map | It said a persistent redisq or sqliteq worker learns of jobs only from its own process. As its only source, the worker also checks the storage once a second (`resyncEvery`). | Fixed: the text depends on whether the storage takes batches (`ops/adapter-data.ts`) |
| 8 | Persistence overview | Same claim (older text). The adapters page says it correctly. | Fixed: the table row and the paragraph give the once-a-second check |
| 9 | Crash test, In memory tab | It showed `DequeueWithAckId`, `Acknowledge` and JSON storage for an in-memory queue, which has none of them. | Fixed: each tab has its own steps and calls (`ops/ack-data.ts`) |

### Majors

| Where | What was wrong | Status |
|---|---|---|
| `/alt/` | It was in the sitemap while it is `noindex`. | Fixed: the sitemap leaves it out |
| `/alt/` hero code | It used the on-paper syntax colours on a dark panel in light mode (2.8 to 3.3:1). | Fixed. The same rule fixes the story's code block |
| `Pause animation` on paper | Pressed, it turned mint on near-white (1.72:1). | Fixed |
| `/alt/` hero pool | Its animation loop never stopped, on or off screen. | Fixed: it runs only while visible |
| Google Fonts | The stylesheet blocks rendering, has no fallback metrics, and sends readers' IPs to Google. | Open, see the questions |
| Errors scene | It said a plain worker has no handle. `Add` returns one. It has no `Err()`. | Fixed |
| Errors scene | The keyboard could pick the disabled "fails" for a plain worker. | Fixed: `tabs()` skips disabled items |
| REST API explorer | The page said its replies match the real handler. Typed paths did not. | Fixed: the page claims only what was checked and names what the model leaves out; an empty name segment and an out-of-range number now get the real replies |
| Benchmarks scoreboard | A tie (SQLite WAL size, VarMQ and goqite at 41.22 MB) was counted as a VarMQ lead. | Fixed: a tie is its own outcome on the scoreboard and the map, and the page states the rule |
| Redis scene | After Close then Kill, job 7 was shown in `jobs` and `jobs:nacked` at once. | Fixed: the scene state moved to `ops/redisflow-model.ts`, with tests |

### Minors and nits

The three reviews also found 42 minor issues and 33 nits. Most of the minors are fixed on this branch.

**Fixed**

- **Shell and landing page.**
  - Focus rings keep the element's shape, and the ring is the right colour on the dark install box, the storage scene and the status bar.
  - The hero's Pause keeps focus while the worker pauses.
  - Clicking the tab that is already chosen no longer resets the storage demo.
  - Banners, the home page's tabs and long install commands wrap on a phone.
  - The "off" storage box keeps full text contrast.
  - Toggle buttons say their state once, in the label.
  - The footer's Benchmarks link goes to the site's own page.
  - Two figures on the home page were worded wrongly; they are corrected:
    - the pool throughput is a mean of 15 runs of 1M tasks at three CPU counts;
    - the 0.058 ms median is from 10 workers taking one job at a time.
- **Scenes.**
  - The event loop shows its second goroutine exit.
  - A second `Restart()` while one waits returns `ErrRunningWorker`.
  - The job scene's pending close no longer touches a reset job.
  - PoolScene's frame loop and IdleScene's interval stop when nothing plays.
  - Under reduced motion, the context and idle scenes run to their end state.
  - Wording fixes on the strategies, queues, job IDs and topics pages.
- **Ops, benchmarks and reference.**
  - The redisq and sqliteq tabs show the batch pop.
  - A restart no longer moves a job between processes.
  - Focus moves on when a pressed button hides itself.
  - Several claims are corrected:
    - the 4-CPU exception on the Benchmarks page;
    - `Submitted` on persistent and distributed queues;
    - the "Redis only" and "in-memory only" limits;
    - the processing ratio just after `TunePool`;
    - which repository has `compose.yml`.
  - The benchmark components no longer import `tokens.css`.

**Open**

- **Shell and landing page.**
  - Replay under reduced motion, and the hero's seeded jobs under reduced motion.
  - Tab panels that stay hidden without JavaScript.
  - Expressive Code marker colours, which no page uses yet.
  - Some local copies of shared pieces. The `.btn` button and the install box are now shared.
- **Scenes.**
  - The strategy captions lack `WithAutoRun(false)`.
  - A capacity change applies to a queue that already holds jobs.
  - The concurrency and metrics scenes keep a Step button under reduced motion, because they teach the state while jobs run.
- **Ops.**
  - The Redis scene's presence key, and its dead consumer coming back.
  - A live region that is rebuilt ten times a second.
  - The metrics replay under reduced motion.
- **Nits:** 22 of the 33 are open.

### Design notes

These are matters of taste, not bugs:

- **The `JOB nn` banner over every page title is a label above a heading.** It adds a second, smaller title in front of the real one. `JOB` is also the library's word for a queued item: the "Jobs" page carries a `JOB 02` chip. A plain section number, or no banner, says less that is wrong.
- **Some labels lean on the paper idea rather than saying what is there.** Examples: "tear here", "same worker, three homes". No explanatory sentence relies on it.
- **The page background's repeating stripes** are a common generated-UI pattern (the design detector flags them as advisory). On the home page they no longer show behind the story.
- **"Every scene runs the real model" on `/alt/`.** Its three small scenes replay fixed frames. The frames are correct.

## Verification of `docs/new-site`

- `pnpm test`: 209 tests pass, including the new tests for the scene models, the ops scenes and the benchmark ties.
- `pnpm build`: 31 pages. The sitemap leaves out `/alt/`.
- Every built page, at 1440 and 390 px wide, in light and dark (120 views):
  - no console error;
  - no sideways scroll;
  - one h1;
  - the chosen theme applied.
- The home page story at 18 viewport sizes, from a 320 px phone to a 1608 px window, in both themes:
  - The intro and every step fit their stage, with no sideways scroll.
  - The lit code lines stay in view, except at the three shortest sizes (667 × 325, 844 × 340 and 375 × 553). There one or two lit lines sit under the code window's fade.
- Scroll input on the home page:
  - Tested with wheel events at real timing inside the page, with trusted wheel and key input from the browser protocol, and with touch drags.
  - A notch, a flick or a swipe moves one step, and a free-spinning wheel keeps stepping.
  - A scroll back up plays each transition backward and lands on the step's end exactly as it was left going down (pixel-identical), at 60 fps: each frame seeks forward from that end, and puts back what the frame before changed (`lib/story/map.ts`, `after`), since a full seek redraws the loop from 0 and anime.js going backward keeps stale values.
  - A free spin recorded from an MX Master on Linux replays through every step. Before, it stopped after four: the story measured the wheel's speed per event, and Chrome sends the notches it holds back during a step's first frames as one event (`lib/story/wheel.ts`, with tests).
  - From the last step down the page scrolls normally, by wheel, keys and touch; on the way back up it stops at the last step, and the story takes over again from there.
- Reduced motion: each step shows its last frame, and nothing animates.
- The design detector reports nothing on the home page.
- The theme switch stores the choice, the choice carries to `/alt/`, and System follows the operating system while the page is open.
- The five scenes changed most were opened and pressed in both themes, with no console error.

Not checked: Safari and Firefox, and screen readers. Neither was `astro check`: the project has no TypeScript checker installed.

## Questions for the owner

1. **Fonts.** The site loads Bricolage Grotesque and JetBrains Mono from Google. The earlier story page used Go Sans and Go Mono, self-hosted. Should the site self-host its fonts, and which pair?
2. **`/alt/`.** The story now opens `/`. Should `/alt/` stay as an alternative, or go?
3. **The `JOB nn` banner.** Keep it, rename it, or drop it?
4. **The home page sections.** "What you get" was removed, and "A closer look" was merged into "Try it". Keep these changes?
