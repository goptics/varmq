# VarMQ docs design contract

Every page of the docs site follows this file. The values live in `src/styles/tokens.css`. The shared pieces live in `src/styles/ui.css` and `src/components/ui/`. The home page `/` (`src/components/Story.astro`, `src/styles/story.css`) shows the system at its fullest.

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
| Type and layout | `--display`, `--mono`, `--gutter`, `--ease`. The landing frame's `--st-max` and `--st-gutter` are set in `story.css` on `:root:has(.st)`, as only `/` uses them. |

All text pairs pass WCAG AA in both themes (lowest ratio 4.75:1).

## Type

- Body and headings use `--display` (Bricolage Grotesque). Code, labels, captions and numbers use `--mono` (JetBrains Mono). The shell `Head` loads both fonts once. A page stylesheet never imports them.
- Body: 1.0625rem, line height 1.6.
- H1 on the landing page: `clamp(2.375rem, 4.4vw, 3.75rem)`, weight 700, optical size 96, letter spacing -0.035em, line height 0.98, `text-wrap: balance`. Where the text column is narrow it steps down (1.875rem on a short phone).
- A step's title on the landing page: `clamp(2rem, 3.2vw, 2.625rem)`, weight 700, optical size 72, letter spacing -0.03em, line height 1.05. The section heads under the story are article h2s (`--local-h2`).
- Articles use a smaller scale (the shell sets `--local-h1`, `--local-h2` in `docs-theme.css`) because the column is 44 rem wide (`--sl-content-width`).
- H3: 1.25rem, weight 650.
- Lede on the landing page: `clamp(1.0625rem, 1.5vw, 1.25rem)`, line height 1.5, `--ink-2`, maximum 40ch.
- Small text on the landing page (the scroll hint, the captions): `--st-small`, 0.9375rem.
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
| Field | `.sfield` (slider, number, text); a bare `select` inside `.sframe` is styled. | `ui.css` |
| Code card | `.code` (+ `.code--dark`), `.code__head` with file name and `.copy` button, then `pre`. `[data-copy]` buttons are served by one handler; `copyText()` copies a computed string. Expressive Code also names each line of a doc code block `.code`, so the card rule skips those. | `ui.css`; `copyButtons()`, `copyText()` in `ui/dom.ts` |
| Go syntax | `highlightGo`, `highlightShell`, `highlightJson` return `tk-*` spans. | `components/ui/go.ts` |
| Doc code block | Expressive Code theme `varmq-panel`. Same tokens as the code card. | `ec.config.mjs`, `docs-theme.css` |
| Lane | Label, bar track, value, and a "× VarMQ" ratio. The VarMQ lane uses `--brand`. | `.rlane` (`bench.css`); scene lanes stay local (`.llane`, `.gb-*`) |
| Callout | Card with an `--amber` rule for results against VarMQ and a `--brand` rule for tips. Starlight asides, `.bboard` and the matrix notes use it. | `docs-theme.css` |
| Tear line | Dashed rule with "tear here" between the last section and the closing call to action. | `.shell-tear`, `.tear` |
| Theme switch | Three native radios: Light, Dark, System. The choice is stored under `starlight-theme` (`''` for System). It sits in the header, and in the phone header on `/`, which has no sidebar menu. | `components/ui/ThemeSwitch.astro`, `ui.css` (`.tswitch`) |
| Header links | Docs, Guides and Benchmarks before the GitHub link, on `/` only, as it has no sidebar. They hover as the GitHub link does, with a `--bar-2` box, and have no underline. Hidden below 50rem with the GitHub link. | `components/shell/SocialIcons.astro`, `docs-theme.css` (`.home-links`) |
| Main action | `.btn` with `--primary` or `--ghost`, for a page's own call to action on paper (the landing pages). Scene controls stay `.pbtn`. | `ui.css` |
| Install box | The `go get` commands in tabs: varmq, then varmq with redisq or sqliteq. | `components/ui/Install.astro`, `ui.css` (`.install`) |
| Store card | Starlight's link card, its title led by the store's icon: a memory stick for in-memory queues, and the map's SQLite and Redis logos, in the map's colours for them. | `components/StoreCard.astro`, `story.css` (`.store-icon`) |

Scene-specific visuals (job chips, queue rails, tracks, boards) stay in the area folder. They read only tokens.

## Motion and interaction

- Use `--ease`. Keep each animation under 1.2 s, except a loop that the reader can pause.
- Every looping animation has a visible pause button (`.pbtn`). A page ornament that carries no meaning (the home page's rails) holds with the nearest one (the map's Pause) and stops under reduced motion. Honor `prefers-reduced-motion`: show the end state and do not loop. A scene that plays steps offers a Step button.
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
| Landing | `/` | Full width. It keeps the Starlight header and opens on the story (below). Everything under the story shares the stage's frame (up to 1440 px, `--st-max`, with `--st-gutter` at each side), so the edges line up: each section is a row with its heading and lead in the story's text column and the rest in the map's, and stacks at 44rem below 72rem. |
| Article | `start`, `concepts`, `guides`, `persistence`, `operations` | Starlight layout restyled by the shell theme. One banner, then the article. Add a scene where a scene explains faster than text. |
| Reference | `reference/comparison` | Dense tables and a feature matrix that the reader can filter. |
| Data | `reference/benchmarks` | Explorer and score board on dark panels, raw-data links, and the callout for results against VarMQ. Data comes from a committed snapshot (`components/bench/data.json`, checked by `scripts/bench-data.mjs`). |
| 404 | `/404` | Banner `JOB 404`, one sentence, the footer links. |

## The home page story

`/` opens on varmq's story (`components/Story.astro`, `lib/story/`, `styles/story.css`):

- **The stage.** It is pinned under the header, with the text on the left and the map on the right. The text column is a size container, so it sets itself by the room it has. On portrait screens below 1024 px the map sits on top and the text below.
- **The intro.** It comes first: the h1 and the lede (the hero fields of `index.mdx`), the install box, Get started and GitHub, and the whole map. The map runs as in the last step, from where that step's camera has pulled back, holds 1.5 s once it is still, and runs again, until the reader scrolls on. Each restart puts back what the run's end stopped (the core's goroutine, the loop's glow, the Redis consumers' rings). Under reduced motion it stands still. Its last frame is where step 1's zoom starts. On portrait screens the map sits on top of the intro as it does on top of every step, at the same size, so nothing moves when the story starts. The text under it keeps 22rem, room for the intro with its lede on a 390 × 664 phone; shorter screens drop the lede.
- **The steps.** Each scroll plays the next caption of one of ten steps: that stretch of its chapter, once, at the map's own pace. A scroll back up within a step plays its last caption backward. A step reached going down plays from its start with every ring empty, up to the caption it stops at; reached going up, the map plays backward from the later step to that caption's finished frame (1 s at most) and holds there, every ring full (back from step 1, the map plays step 1 backward to the intro's still frame, and the intro's run starts again from there at once). The last step then plays the whole map's run over, as the intro does, until the reader scrolls on; while the story is out of view below, that loop rests, and back in view the step plays from its start. The one code block morphs into the code behind that chapter and lights the lines each caption is about. The code block is a dark panel in both themes, highlighted at build time with the same theme as the doc code blocks (`ec.config.mjs`).
- **Captions.** A caption is a button. It scrolls to that caption's stop, or plays the caption again if the page is there. The caption playing is in `--accent-text`; the others, done ones too, are `--ink-3`. Its point is a ring in `--brand`, and a line runs from ring to ring with its ends under the rings' strokes, so the captions read as one track that fills as the step plays.
- **Snapping.** The page snaps one step at a time only while the reader is in the story above its last step. One wheel turn is one step, and a free-spinning wheel keeps stepping. From the last step down the page scrolls normally, with every input, and may rest anywhere. On the way back up it scrolls normally too, until a wheel turn or a key would pass the last step: it stops there, and from there the story takes over again, a step at a time.
- **Skip.** One round 48 px button, fixed at the bottom centre of the screen (in the corner under the map when the screen is under 30rem tall), shows on steps 1 to 9. It jumps to step 10. Its outline is ten segments, one per step, that fill in `--brand` as the steps play. Focus on it moves to step 10's title when it goes.
- **Pause.** A `.pbtn--paper` button in the map frame's top corner holds the map still (`aria-pressed`, its label turns to Play) and plays it on from there. Paused, the scroll hint's arrow and the rails' drift hold still too. Under reduced motion the map is still already, so the button is hidden.
- **The scrollbar.** The home page hides it (`scrollbar-width: none` on `:root:has(.st)`). The page still scrolls with the wheel, keys and touch, and the glow on the rails shows where the reader is.
- **The sprocket-hole rails.** From 1180 px up, a 36 px rail runs down each side of the window, fixed, the window's full height and over the header too, with a dashed `--rule` edge toward the page. The header's padding there is 52 px, so it clears them, and the main pane drops Starlight's isolation so the rails draw over the header. A rail has one hole every 48 px, in its middle: punched, a little darker than the paper, with a thin `--brand` rim. The holes feed as paper does: the left rail's down and the right rail's up, 8 px a second on their own and a third as far as the page has scrolled. A 168 px glow band of lit holes in `--brand` runs down both rails from the top of the page to the bottom. `Hero.astro` eases the holes' offset and the band's place toward the scroll position each frame, so they glide after a snap, and moves them by transform only. Below 1180 px there are no rails. Under reduced motion the holes stand still and unlit.
- **Reduced motion.** Each step shows its last frame.
- **Checks.** Check every step at the 18 viewport sizes in `home-stage-fit-testing`, both themes. Test the wheel inside the page, not through CDP events alone.

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
- **Where it lives.** The palette is set on `:root`, so the story's text beside the map uses the same red, SQLite blue and Redis red (`--vm-*-text`).

## Adding a page or a component

1. Put a scene in `src/components/<area>/` and import `ui/Scene.astro`. Import your area stylesheet in the scene's frontmatter. Do not import `tokens.css` or fonts.
2. Need a button, tab, field, code card or highlighter? Use the shared one. Need a new shared piece? Add it to `ui.css` or `components/ui/`, then list it above.
3. A value that no token carries is a `--local-` variable inside your component. If two areas need it, promote it to `tokens.css`.
4. No hex, `rgb()` or `hsl()` outside `tokens.css`. The design detector (`impeccable detect src`) must report nothing.
5. Before you merge: `pnpm build`, `pnpm test`, then open the page at 1440 px and 390 px in light and dark. No console error, no horizontal scroll.

## Decisions and known gaps

- Theme: Light, Dark or System, from the header's switch; System follows `prefers-color-scheme`. `starlight-theme-rapide` is removed.
- The Go call strip sits under the stage in every scene (it sat on top in two areas).
- Selected tab = `--panel-ink` fill on a dark panel, `--ink` on paper. The brand fill is for primary buttons only.
- The map on `/` (`src/lib/heromap/map.css`) keeps its own `--vm-*` palette (see "The map's two palettes"). The dark palette's hex values and the light palette's amber, violet, red and blue are not tokens.
- Scene chips and lanes (`.ljob`, `.llane`, `.ga-*`, `.gb-*`, `.ops-job`) are still local. They read tokens, but each area has its own markup.
- `GoPlay.astro` uses Starlight `--sl-*` variables, which the shell maps to the tokens. Its buttons are `.pbtn`.
- The explorer on the Benchmarks page and the matrix on the Comparison page keep their own table markup.
- The rails' values (`--hole`, `--rim`, `--pitch`, `--band`) are local to `.roll` in `story.css`, without the `--local-` prefix. The band's fade mask writes `black`; a mask reads only its alpha, so it is not a colour.
- The home page has two h2 sizes: a step's title, and the section heads under the story at the article size.
