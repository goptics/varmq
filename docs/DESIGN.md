# VarMQ docs design contract

Every page of the docs site follows this file. The values live in `src/styles/tokens.css`. The shared pieces live in `src/styles/ui.css` and `src/components/ui/`. The home page `/` (`src/components/Story.astro`, `src/styles/story.css`, and the sections under it in `src/components/home/`, `src/styles/home.css`) shows the system at its fullest.

## The idea

The queue is a roll of continuous-feed paper. A page is a print job. One square is one job. One lane is one worker.
A page explains by showing: each claim is a small working scene placed next to the Go call that makes it.

## Tokens

Use only the variables in `src/styles/tokens.css`. Never write a hex color in a page. The reader picks Light, Dark or System in the header's theme switch. System follows `prefers-color-scheme`. The tokens switch on `:root[data-theme='dark']`. Starlight's ThemeProvider sets `data-theme` before the first paint from the stored choice, so it carries from page to page.

| Role | Variables |
|---|---|
| Surface (paper) | `--paper`, `--bar` and `--bar-2` for the 48 px stripes |
| Text | `--ink`, `--ink-2`, `--ink-3`; `--accent-text` for links and highlights on paper |
| Lines | `--rule` (dashed for banners and tear lines, solid for cards) |
| Brand | `--brand` for fills, marks and primary buttons, `--brand-hover` for the hover state, `--brand-ink` for text on `--brand`. Each theme's brand is its own logo's green (`assets/logo-light.svg`, `logo-dark.svg`): `#2e9c7f` in light and `--brand-on-dark` (`#44bf9e`) in dark. On paper the light one has 3.1:1, enough for a mark or a fill, so `--accent-text` is the same hue darker, at 5.2:1, for text. Light paper keeps a clear mint tint (OKLCH chroma 0.025, hue 165), as dark paper keeps its green, so both themes read as the same green. |
| Signal | `--amber` for "against VarMQ" and warnings, `--cobalt` for secondary data, `--danger` for errors |
| Dark surfaces | `--panel`, `--panel-2`, `--panel-ink`, `--panel-ink-2`, `--panel-rule`, `--panel-accent`; `--panel-fill` and `--panel-fill-2` for control and track fills. Code and simulation panels are dark in both themes. |
| Go syntax | `--tk-k`, `--tk-s`, `--tk-c`, `--tk-n`, `--tk-f`, `--tk-t`, `--tk-b` (follow the theme). `--tk-*-on-dark` are the dark values. `ui.css` pins `--tk-*`, and `--brand` to `--brand-on-dark`, inside `.sframe`, `.code--dark`, `.expressive-code`, `.on-dark` (any other dark surface) and `.st-code`, so a dark panel looks the same in both themes. |
| Type and layout | `--display`, `--mono`, `--gutter`, `--ease`. The landing frame's `--st-max` and `--st-gutter` are set in `story.css` on `:root:is([data-home], :has(.st))`, as only `/` uses them. |

All text pairs pass WCAG AA in both themes (lowest ratio 4.75:1).

## Type

- Body and headings use `--display` (Bricolage Grotesque). Code, labels, captions and numbers use `--mono` (JetBrains Mono). The shell `Head` loads both fonts once. A page stylesheet never imports them.
- Body: 1.0625rem, line height 1.6.
- H1 on the landing page: `clamp(2.375rem, 4.4vw, 3.75rem)`, weight 700, optical size 96, letter spacing -0.035em, line height 0.98, `text-wrap: balance`. Where the text column is narrow it steps down (1.875rem on a short phone).
- A step's title on the landing page: `clamp(2rem, 3.2vw, 2.625rem)`, weight 700, optical size 72, letter spacing -0.03em, line height 1.05. The section heads under the story: `clamp(1.875rem, 4.2vw, 3.25rem)`, weight 700, optical size 72, letter spacing -0.03em, line height 1.02 (`.h2`, `home.css`).
- Articles use a smaller scale (the shell sets `--local-h1`, `--local-h2` in `docs-theme.css`) because the column is 44 rem wide (`--sl-content-width`).
- H3: 1.25rem, weight 650.
- Lede on the landing page: `clamp(1.0625rem, 1.5vw, 1.25rem)`, line height 1.5, `--ink-2`, maximum 40ch.
- Small text on the landing page (a step's source link): `--st-small`, 0.9375rem.
- Labels: mono, 0.75rem, uppercase, letter spacing 0.1em.

## Layout

- An article's column is 44 rem (`--sl-content-width`) with its own padding. The landing frame is `--st-max` (1440px) with `--st-gutter` at each side: the `--gutter`, and at least 52px from 1180px up so it clears the sprocket-hole rails. No horizontal scroll from 375 px up.
- The page background is the paper stripe (`--paper` with `--bar` bands of 48 px). Long reading text sits on a plain `--paper` column so stripes never run behind a paragraph. The home page has no stripes (`body` has no background image there): its paper is plain `--paper`, and its rails carry the paper look.
- Radius: 4 px for chips and kbd, 8 px for buttons, fields and cards, 12 px for code cards and segmented tabs, 16 px for scene frames and data panels.
- A scene measures its own width with `@container sframe`, not the viewport.

## Components

Every component has one look. Use the shared piece. Do not write a second copy in a page folder.

| Component | Markup | Files |
|---|---|---|
| Banner (`JOB nn`) | Added by the shell above each title. Never write it in `.mdx`. Number = position in the section. | `components/shell/PageTitle.astro`, `docs-theme.css` |
| Scene frame | `<Scene id title tag describe call>` with slots `stats`, default (stage), `call`, `controls`, `note`. Order: title bar, stage, Go call strip, controls, note. `describe` is the text alternative. | `components/ui/Scene.astro`, `ui.css` (`.sframe*`) |
| Button | `.pbtn`; `--primary` (brand), `--danger` (red outline), `--paper` (on paper, outside a panel); `aria-pressed` for a toggle. Pause and replay controls use `.pbtn`. `.stepper` for a number stepper. A touch target is 44 px on a coarse pointer. | `ui.css` |
| Segmented tabs | `.seg` with `role="tablist"` + `role="tab"`, or `role="radiogroup"` + `role="radio"`; `data-key` per item. Arrow keys, Home, End. | `ui.css`; `tabs()` in `ui/dom.ts` |
| Field | `.sfield` (slider, number, text); a bare `select` inside `.sframe` is styled, and inside the benchmark explorer (`.bench select`, `bench.css`) with the same look. | `ui.css` |
| Code card | `.code` (+ `.code--dark`), `.code__head` with file name and `.copy` button, then `pre`. `[data-copy]` buttons are served by one handler; `copyText()` copies a computed string. Expressive Code also names each line of a doc code block `.code`, so the card rule skips those. | `ui.css`; `copyButtons()`, `copyText()` in `ui/dom.ts` |
| Go syntax | `highlightGo`, `highlightShell`, `highlightJson` return `tk-*` spans. | `components/ui/go.ts` |
| Doc code block | Expressive Code theme `varmq-panel`. Same tokens as the code card. | `ec.config.mjs`, `docs-theme.css` |
| Lane | Label, bar track, value, and a "× VarMQ" ratio. The VarMQ lane uses `--brand`. | `.rlane` (`bench.css` under `.bench`, `home.css` under `.home-jobs`); scene lanes stay local (`.llane`, `.gb-*`) |
| Callout | Card with an `--amber` rule for results against VarMQ and a `--brand` rule for tips. Starlight asides and the matrix notes use it. | `docs-theme.css` |
| Tear line | Dotted rule with "tear here" between the last section and the closing call to action: the footer's on a docs page, the closing's on `/`, where the footer's is hidden so there is one. | `.shell-tear` (`docs-theme.css`), `.tear` (`home.css`) |
| Theme switch | Three native radios: Light, Dark, System. The choice is stored under `starlight-theme` (`''` for System). It sits in the header, and in the phone header on `/`, which has no sidebar menu. | `components/ui/ThemeSwitch.astro`, `ui.css` (`.tswitch`) |
| Header links | Docs, Guides and Benchmarks before the GitHub link, on `/` only, as it has no sidebar. They hover as the GitHub link does, with a `--bar-2` box, and have no underline. Hidden below 50rem with the GitHub link. | `components/shell/SocialIcons.astro`, `docs-theme.css` (`.home-links`) |
| Main action | `.btn` with `--primary` or `--ghost` (a `--rule` outline, `--bar-2` on hover), for a page's own call to action on paper (the landing pages). Scene controls stay `.pbtn`. | `ui.css` |
| Install box | The `go get` commands in tabs: varmq, then varmq with redisq or sqliteq. | `components/ui/Install.astro`, `ui.css` (`.install`) |
| Home section | A print job under the story: `.banner` (`JOB nn`, name, dashed rule, then a meta label), `.section__head` (h2 and lead), then the body. `.on-dark` on a dark panel or band. | `components/home/`, `home.css` |
| Small map | A piece of the hero map in a home section: a `.vm-mini` frame (the map's dot grid, a `--rule` border, no panel) around an SVG drawn by `components/home/mini.ts` with the map's own pieces and classes (chips, lanes, trays, cards, the event loop, stores, goroutine rings, job squares with their trails). `heromap/map.css` styles `.vm-mini` as it styles `varmq-map`, so it takes the map's palette for the theme. The glow and the lanes' edge are `#vm-mini-glow` and `#vm-mini-lane-edge`, once on the page (`Scenes.astro`). The page ships each one's first frame, drawn at build time with `heromap/still.ts`'s small document. | `components/home/mini.ts`, `home.css` (`.vm-mini`) |

Scene-specific visuals (job chips, queue rails, tracks, boards) stay in the area folder. They read only tokens.

## Motion and interaction

- Use `--ease`. Keep each animation under 1.2 s, except a loop: a scene's loop has a Pause, and the home page's maps are the exceptions (below).
- Every looping animation in a scene has a visible pause button (`.pbtn`). On the home page the intro's map runs the whole network in a loop, with no Pause (the owner's choice; a known gap against WCAG 2.2.2, below), and rests while the hero is out of view or under the header. On the tour each step plays once. The small maps under the story have no Pause either (the owner's choice, the same gap): JOB 02's three scenes each loop on their own timeline while in view, and JOB 03's stage runs its jobs while in view; both rest out of view. Under reduced motion each scene shows one still frame that still shows its order, and the stage's buttons jump to their end state. The rails, an ornament, drift on their own. The map and the rails stop under reduced motion. The first paint does not fade in: the hero and its map show at once. Honor `prefers-reduced-motion`: show the end state and do not loop. A scene that plays steps offers a Step button.
- Interactive scenes work with the keyboard. Each control has a visible focus ring: 2 px, 3 px offset, `--accent-text` on paper and `--panel-accent` on a dark panel.
- A scene shows the real model. It does not draw a result that the library cannot produce.

## Content rules

- Do not invent features, numbers, customers or quotes. A number comes from the library source, the docs, or `github.com/goptics/benchmarks`.
- Show results that go against VarMQ next to the results that favor it.
- Memory figures are MB = 10^6 bytes on every page. `benchmarks.goptics.org` divides by 2^20, so its MB figure is 4.6 % lower. The Benchmarks page says so.
- Write for Go developers. Use short sentences and plain words. Keep API names in code format.
- Keep existing facts, headings and anchors of a docs page unless the change is the point of the task. Do not break inbound links.

## Accessibility

- Text contrast is AA in both themes. Do not use color alone to carry meaning.
- Each interactive scene has a text alternative that states what it shows (`describe`).
- Use real headings in order. Use landmarks. Keep the skip link.
- A touch target is at least 44 px on a coarse pointer. The `@media (pointer: coarse)` block at the end of `ui.css` sizes the shared pieces (`.pbtn`, tabs, fields, copy buttons, the theme switch), and the one in `docs-theme.css` sizes Starlight's code copy and search buttons. A new control joins one of those lists.

## Page types

| Type | Used by | Rule |
|---|---|---|
| Landing | `/` | Full width. It keeps the Starlight header and opens on the intro beside the map, with the story as a tour the reader can take (below). Everything under the story shares the stage's frame (up to 1440 px, `--st-max`, with `--st-gutter` at each side), so the edges line up. The sections under it are print jobs (below, "The sections under the story"). |
| Article | `start`, `concepts`, `guides`, `persistence`, `operations` | Starlight layout restyled by the shell theme. One banner, then the article. Add a scene where a scene explains faster than text. |
| Reference | `reference/comparison` | Dense tables and a feature matrix that the reader can filter. |
| Data | `reference/benchmarks` | The explorer on a dark panel, results first: the dataset tabs (one line in the 44 rem column, two rows of three on a phone), one wrapping row of controls, then the scope line, which says in how many groups VarMQ leads, and the lanes, the map of who leads each group, the callout for results against VarMQ, every row, and Run details last. The controls are a labelled `select` per filter of the dataset and for Order, Show as a segmented choice (it changes what the lanes measure), a Channel baselines toggle on the pool datasets, and Clear filters, shown only while something is filtered. Run details is one closed disclosure that holds the shell command for the groups in scope with Copy, the run's date, commit, versions, machine and raw-file links. Data comes from a committed snapshot (`components/bench/data.json`, checked by `scripts/bench-data.mjs`). |
| 404 | `/404` | Banner `JOB 404`, one sentence, the footer links. |

## The home page story

`/` opens on the intro beside the map, and offers varmq's story as a tour of ten steps (`components/Story.astro`, `lib/story/`, `styles/story.css`):

- **The page.** By default it is the hero, then the sections under it, and it scrolls as any page does: no snapping, nothing pinned. The hero has the text on the left and the map on the right; on portrait screens below 1024 px the map sits on top and the text below. It is one screen under the header, pinned only on the tour, and its text column is a size container, so it sets itself by the room it has.
- **The intro.** The h1 and the lede (the hero fields of `index.mdx`). The h1 names two parts of the map beside it in their colours as text: "Worker pools" in the goroutines' green (`--vm-go`) and "job queues" in the amber edge of its queues and jobs (`--vm-job-edge`), the map's own colours in each theme; the rest stays `--ink`. As large text its floor is 3:1: light 3.1:1 and 4.35:1, dark 8:1 and 9.8:1. Then the install box and Get started (the GitHub link is in the header, on phones too), and the whole map with the tour button on it. The map's still frame is in the HTML at first paint (`lib/heromap/still.ts`); once the animation's code has loaded, the map runs as in the last step, from where its camera has pulled back, holds 1.5 s once still, and runs again, until the reader takes the tour. It rests while the hero is out of view or under the header, and runs again from its start when it is back. On portrait screens the map sits on top of the intro as it does on top of every step, at the same size, so nothing moves when the tour starts. The text under the map keeps 22rem, room for the intro with its lede on a 390 × 664 phone; shorter screens drop the lede.
- **The tour.** A `.pbtn--paper` button, "Take the tour · 10 steps" with a play mark, sits at the foot of the map, centred 10 px above its edge, so it is on the first screen wherever the map is. On a map under 17rem wide it reads "Take the tour". It gives the story's track its 27 stops (the intro and one per caption), pins the stage under the header, plays step 1, and moves focus to its title. Without JS it is hidden (`shell/Head.astro`); the story as text stays for screen readers.
- **The steps.** Each scroll plays the next caption of one of ten steps: that stretch of its chapter, once, at the map's own pace. A scroll back up within a step plays its last caption backward. A step reached going down plays from its start with every ring empty, up to the caption it stops at; reached going up, the map plays backward from the later step to that caption's finished frame (1 s at most) and holds there, every ring full. Back up from step 1 the tour ends at the intro, and the map plays backward to the end of its run, then loops. Step 10 plays once and holds its last frame. The one code block morphs into the code behind that chapter and lights the lines each caption is about. The code block is a dark panel in both themes, highlighted at build time with the same theme as the doc code blocks (`ec.config.mjs`).
- **Captions.** A caption is a button. It scrolls to that caption's stop, or plays the caption again if the page is there. The caption playing is in `--accent-text`; the others, done ones too, are `--ink-3`. Its point is a ring in `--brand`, and a line runs from ring to ring with its ends under the rings' strokes, so the captions read as one track that fills as the step plays.
- **Snapping.** On the tour the page snaps one stop at a time above its last step. One wheel turn is one stop, and a free-spinning wheel keeps stepping. From step 10 down the page scrolls normally, with every input; on the way back up, a wheel turn or a key that would pass step 10 stops there, and the story takes over again.
- **Leaving the tour.** There is no Exit button. Skip, one round 48 px button fixed at the bottom centre of the screen (in the corner under the map when the screen is under 30rem tall), shows on steps 1 to 10. Its outline is ten segments, one per step, that fill in `--brand` as the steps play, and its name says it skips the rest of the tour. It ends the tour: the track gives up its stops, the map goes back to the intro's loop (resting while the hero is out of view), and the page jumps to the first section's heading, which takes focus. Esc does the same. A scroll on past step 10 into the sections ends the tour too, where the reader is: the page moves up by what the track gave up, so the sections in view stay put, and back up the page reaches the hero, not the story. Back up at the intro (stop 0), at the foot of a page too short to scroll a stop past step 10, or tabbing out into the sections, the tour ends the same way, where the reader is; focus left in the story moves to what is in view: the h1 at the top, past the end the first section heading wholly on screen, or nothing if none is. Tabbing out keeps focus where the Tab put it and brings it into view. Esc in the search dialog only closes the dialog. A reload mid-tour opens on the page without it, at its top, and the page keeps its scroll on later reloads. The tour can start again from the map.
- **The scrollbar.** From 1180px up, where the rails show, the home page hides it (`scrollbar-width: none` on `:root:is([data-home], :has(.st))`); narrower, the rails are hidden and the browser's scrollbar shows. The page still scrolls with the wheel, keys and touch, and the glow on the rails shows where the reader is.
- **The sprocket-hole rails.** From 1180 px up, a 36 px rail runs down each side of the window, fixed, the window's full height and over the header too, with a dashed `--rule` edge toward the page. The header's padding there is 52 px, so it clears them, and the main pane drops Starlight's isolation so the rails draw over the header. A rail has one hole every 48 px, in its middle: punched, a little darker than the paper, with a thin `--brand` rim. The holes feed as paper does: the left rail's down and the right rail's up, 8 px a second on their own and a third as far as the page has scrolled. They are an ornament, the one thing on the page that moves without the reader. A 168 px glow band of lit holes in `--brand` runs down both rails from the top of the page to the bottom. `Hero.astro` eases the holes' offset and the band's place toward the scroll position each frame, so they glide after a snap, and moves them by transform only. Below 1180 px there are no rails. Under reduced motion the holes stand still and unlit.
- **Reduced motion.** The tour is still offered. Each step shows its last frame.
- **Checks.** Check the hero and every step at the 18 viewport sizes in `home-stage-fit-testing`, both themes. Test the wheel inside the page, not through CDP events alone.

### The sections under the story

Under the hero, `index.mdx` holds one `.home-jobs.not-content` wrapper with a component per section (`src/components/home/`, `src/styles/home.css`). `not-content` keeps the docs' prose rules off them, and every rule in `home.css` is scoped by `.home-jobs`, except the two that hide the footer's tear line on `/`, scoped by `:root:is([data-home], :has(.st))`. The colours are the tokens, so they follow the theme switch with the rest of the page. Each section is a `section.home-row` with a `.wrap` (the story's frame) and its banner: the tour ends at the first one, and focuses `#try-it`, JOB 05's heading.

1. **JOB 02 Scheduling** (`Scenes.astro`, `scenes.ts`, `order.ts`). Three small maps (`mini.ts`), each with its own looping anime.js timeline and the Go call of each step as its caption above it: a priority queue (`WithAutoRun(false)`, Add report 10, alert 1, digest 10, then Start: alert runs, then report and digest in the order added; each square shows its number alone, and the captions name the jobs), three queues on one worker under RoundRobin (alerts 2, reports 1, emails 3, as `examples/strategies-round-robin`), and a topic with two subscribed workers (one Publish, a job for each). The order comes from `order.ts`, which takes jobs as varmq does (the priority queue's ties by insertion, `helpers.Manager.GetRoundRobinItem`), pinned by `order.test.ts`. A job that has run leaves the scene at the worker, as on the hero map, so the order shows in the order jobs reach the worker and in the captions, which end on `// ran: ...`. Each scene plays only while it is in view and holds its end briefly before it runs again; reduced motion shows one still frame, the first job on the worker and the rest waiting, captioned with the order it shows. The scenes and the storage stage draw with the map's classes, which `heromap/map.css` styles under `.vm-mini` as under `varmq-map`, on the page's paper (no dark panel).
2. **JOB 03 Where jobs live** (`Storage.astro`, `store.ts`). Tabs for memory, Redis and SQLite; a small map with your process (Add() and the worker) and the storage under it, Restart process and Add 5 jobs, the result in an `aria-live` line; the code that picks the store beside it. In memory the queue is in the process, and a restart loses the waiting jobs and the running one. With SQLite or Redis the queue and an IN FLIGHT tray are in the store: the waiting jobs stay. The running job's record stays in flight; sqliteq puts it back when the restarted program opens the queue, and redisq once it sees the crashed run is gone, at the restart in the demo as with SQLite. That is the behaviour of the redisq change that takes a plain queue's owner for gone when its wake-channel subscription drops; redisq v1.5.0 still waits for the 30 s heartbeat, so this ships with that release. A job put back waits at the front for 0.7 s before the worker takes it, and the line adds "Job N runs again now." The worker takes the next job as soon as it is free, during an add too. The result line sits under the buttons, so they never move. Below 480 px the stage stacks the store under the process.
3. **JOB 04 Measured in public** (`Race.astro`, `race.ts`). On the page's own paper in both themes (no dark band). Tabs for the worker pool, Redis and SQLite, lanes that count up when first seen and on Replay (`Lanes.astro`), the results against VarMQ in one toggle (a `details`, closed by default, its amber summary the heading), then the run. Every figure is computed from `bench/data.json` by `bench/home.ts` (pinned by `home.test.ts`).
4. **JOB 05 Programs** (`Examples.astro`). Five example programs in segmented tabs (Quick start, Priority, Round robin, Topics, Results), each in a code card with its file name and its Run, Edit, Reset and Playground ↗ buttons in its head, the lines before `func main` folded, and a "Read it in the docs" link after the tab's description. Each one runs in place: the card is a `go-play` element (`src/lib/go-play.ts`, the one `GoPlay.astro` uses; both read the programs through `src/lib/examples.ts`). Run sends the program to the Go Playground and plays its output back in an Output panel beside the card (under it below 900 px); Edit swaps the code for a textarea inside the card, with the same type, and Reset brings the code back; Open in Go Playground opens it there. In the editor Tab inserts a tab, Esc moves focus to Run and Ctrl+Enter runs. The heading is `#try-it`, where the tour ends.
5. **JOB 06 Index** (`Index.astro`). Seven rows into the docs: Start, Concepts, Guides, Persistence, Operations, Comparison, API reference.
6. **The closing** (`Close.astro`). The tear line, "Run your first worker.", the install box, Quick start and Read the docs. The footer under it starts with a dashed rule instead of its own tear line.

### The map's two palettes

The map keeps its own palette (`lib/heromap/map.css`), in both themes:

- **Dark.** The map's original palette, which is not to be changed: amber jobs, green goroutines, a violet event loop, red failures, SQLite blue and Redis red.
- **Light.** The same hues, made dark enough to read on `--paper`:
  - Every text colour, and the strokes of every hue but green, has an OKLCH lightness of 0.50 to 0.56, the same as the page's own on-paper colours (`--accent-text`, the `--tk-*` values). At equal lightness no hue looks heavier than another, and each has at least 4.5:1 against `--paper`.
  - The green of a worker, its goroutines and their work is the light logo's (`--brand`), as the dark map's is the dark logo's, so the map and the logo share one green. As a mark it needs 3:1, and has 3.1:1.
  - Fills use the same hues at a lightness of about 0.96.
  - The greys come from the page's green-tinted inks (`--ink-3`, `--ink`) rather than a cool grey, so the map matches the paper around it.
  - A job keeps the dark map's amber (`#f2b036`) with a darker edge (`#a26500`), which gives the square its 3:1 against the paper.
  - The light palette uses tokens where the page has one: `--brand` for green marks, `--accent-text` for green text, `--tk-s` for the amber text, and `--ink-3` for labels and lines.
- **Where it lives.** The palette is set on `:root`, so the story's text beside the map uses the same red, SQLite blue and Redis red (`--vm-*-text`), and the small maps under the story (`.vm-mini`) draw in it.

## Adding a page or a component

1. Put a scene in `src/components/<area>/` and import `ui/Scene.astro`. Import your area stylesheet in the scene's frontmatter. Do not import `tokens.css` or fonts.
2. Need a button, tab, field, code card or highlighter? Use the shared one. Need a new shared piece? Add it to `ui.css` or `components/ui/`, then list it above.
3. A value that no token carries is a `--local-` variable inside your component. If two areas need it, promote it to `tokens.css`.
4. No hex, `rgb()` or `hsl()` outside `tokens.css`. The design detector (`impeccable detect src`) must report nothing.
5. Before you merge: `pnpm build`, `pnpm test`, then open the page at 1440 px and 390 px in light and dark. No console error, no horizontal scroll.

## Decisions and known gaps

- Theme: Light, Dark or System, from the header's switch; System follows `prefers-color-scheme`. `starlight-theme-rapide` is removed.
- The Go call strip sits under the stage in every scene (it sat on top in two areas).
- The home page's intro map loops with no Pause button, by the owner's choice. That falls short of WCAG 2.2.2 (Pause, Stop, Hide) for readers who have not set reduced motion; a small Pause on the map frame would close it.
- JOB 02's three scenes loop, and JOB 03's stage keeps running jobs, with no Pause, by the owner's choice: the same gap against WCAG 2.2.2 for readers who have not set reduced motion. Both stop out of view. A Pause in each banner would close it.
- Selected tab = `--panel-ink` fill on a dark panel, `--ink` on paper. The brand fill is for primary buttons only.
- The map on `/` (`src/lib/heromap/map.css`) keeps its own `--vm-*` palette (see "The map's two palettes"). The dark palette's hex values and the light palette's amber, violet, red and blue are not tokens.
- Scene chips and lanes (`.ljob`, `.llane`, `.ga-*`, `.gb-*`, `.ops-job`) are still local. They read tokens, but each area has its own markup.
- `GoPlay.astro` uses Starlight `--sl-*` variables, which the shell maps to the tokens. Its buttons are `.pbtn`. The `go-play` element itself (Run, Edit, Reset, Open, the output) is `src/lib/go-play.ts`; `GoPlay.astro` and the home page's `Examples.astro` give it their own markup and styles.
- The explorer on the Benchmarks page and the matrix on the Comparison page keep their own table markup.
- The rails' values (`--hole`, `--rim`, `--pitch`, `--band`) are local to `.roll` in `story.css`, without the `--local-` prefix. The band's fade mask writes `black`; a mask reads only its alpha, so it is not a colour.
- The home page has two h2 sizes: a step's title, and the section heads under the story (`.h2`), larger, as the print jobs' titles. The closing's heading uses the display face at `clamp(2.25rem, 6vw, 4.5rem)`.
