# VarMQ docs design contract

Every page of the docs site follows this file. The values live in `src/styles/tokens.css`. The reference page is `/alt/` (`src/pages/alt/index.astro`, `src/styles/alt.css`, `src/components/alt/`).

## The idea

The queue is a roll of continuous-feed paper. A page is a print job. One square is one job. One lane is one worker.
A page explains by showing: each claim is a small working scene placed next to the Go call that makes it.

## Tokens

Use only the variables in `src/styles/tokens.css`. Never write a hex color in a page. Light and dark follow `prefers-color-scheme`.

| Role | Variables |
|---|---|
| Surface (paper) | `--paper`, `--bar` and `--bar-2` for the 48 px stripes, `--hole` |
| Text | `--ink`, `--ink-2`, `--ink-3`; `--accent-text` for links and highlights on paper |
| Lines | `--rule` (dashed for banners and tear lines, solid for cards) |
| Brand | `--brand` for fills and primary buttons, `--brand-ink` for text on `--brand` |
| Signal | `--amber` for "against VarMQ" and warnings, `--cobalt` for secondary data, `--danger` for errors |
| Dark surfaces | `--panel`, `--panel-2`, `--panel-ink`, `--panel-ink-2`, `--panel-rule`, `--panel-accent`. Code and simulation panels are dark in both themes. |
| Go syntax | `--tk-k`, `--tk-s`, `--tk-c`, `--tk-n`, `--tk-f`, `--tk-t`, `--tk-b` |
| Type and layout | `--display`, `--mono`, `--gutter`, `--max`, `--ease` |

## Type

- Body and headings use `--display` (Bricolage Grotesque). Code, labels, captions and numbers use `--mono` (JetBrains Mono).
- Body: 1.0625rem, line height 1.6.
- H1: `clamp(2.5rem, 7.4vw, 5.75rem)`, weight 700, optical size 96, letter spacing -0.035em, line height 0.98, `text-wrap: balance`.
- H2: `clamp(1.875rem, 4.2vw, 3.25rem)`, weight 700, optical size 72, letter spacing -0.03em, line height 1.02.
- H3: 1.25rem, weight 650.
- Lede: `clamp(1.125rem, 1.8vw, 1.375rem)`, `--ink-2`, maximum 46ch.
- Labels: mono, 0.75rem, uppercase, letter spacing 0.1em.

## Layout

- Content width is `--max` (1180px) with a `--gutter` side gutter. No horizontal scroll from 375 px up.
- The page background is the paper stripe (`--paper` with `--bar` bands of 48 px). Long reading text sits on a plain `--paper` column so stripes never run behind a paragraph.
- Radius: 4 px for chips, 8 px for buttons and cards, 12 px for large panels. When a reference component uses another value, copy that value.

## Components

Every component has one look. If the reference page already has it, copy its markup and class names.

- **Banner**: a `JOB nn` chip (`--ink` on `--paper`), a label, a dashed rule that fills the row, and an optional right-hand label. Every docs page opens with one. The number is the page's position in its section.
- **Button**: primary is `--brand` with `--brand-ink`; ghost has a `--rule` border. Pause and replay controls use the small `pbtn` style.
- **Segmented tabs**: `role="tablist"`, arrow-key navigation, one panel visible.
- **Lane**: label, bar track, value, and a "× VarMQ" ratio. The VarMQ lane uses `--brand`; others use a neutral fill.
- **Scene frame**: a dark panel with a small caption line that shows the Go call. Controls sit under the scene.
- **Code card**: dark panel, file name in the title row, Go syntax tokens, a copy button.
- **Callout**: a card with an `--amber` rule for results that go against VarMQ, and a `--brand` rule for tips.
- **Tear line**: a dashed rule with the words "tear here" between the last section and the closing call to action.
- **Status bar**: only on the landing page. Docs pages keep the Starlight sidebar and header, restyled with these tokens.

## Motion and interaction

- Use `--ease`. Keep each animation under 1.2 s, except a loop that the reader can pause.
- Every looping animation has a visible pause button. Honor `prefers-reduced-motion`: show the end state and do not loop.
- Interactive scenes work with the keyboard. Each control has a visible focus ring (`--accent-text`, 2 px, 3 px offset).
- A scene shows the real model. It does not draw a result that the library cannot produce.

## Content rules

- Do not invent features, numbers, customers or quotes. A number comes from the library source, the docs, or `github.com/goptics/benchmarks`.
- Show results that go against VarMQ next to the results that favor it.
- Write for Go developers. Use short sentences and plain words. Keep API names in code format.
- Keep existing facts, headings and anchors of a docs page unless the change is the point of the task. Do not break inbound links.

## Accessibility

- Text contrast is AA in both themes. Do not use color alone to carry meaning.
- Each interactive scene has a text alternative that states what it shows.
- Use real headings in order. Use landmarks. Keep the skip link.

## Page types

| Type | Used by | Rule |
|---|---|---|
| Landing | `/alt/` | Full width, paper stripes, status bar. |
| Article | `start`, `concepts`, `guides`, `persistence`, `operations` | Starlight layout restyled by the shell theme. One banner, then the article. Add a scene where a scene explains faster than text. |
| Reference | `reference/comparison` | Dense tables and a feature matrix that the reader can filter. |
| Data | benchmarks | Lanes and charts, raw-data links, and the callout for results against VarMQ. |

## Rules for parallel work

1. Do not edit `src/styles/tokens.css`, `src/pages/alt/`, `src/components/alt/` or `src/styles/alt.css`.
2. If you need a new token or a new shared component, define it locally with a `--local-` prefix or a local component in your own folder. List it in your report under "Proposed shared additions". The master session decides.
3. Put your components in `src/components/<your-area>/` and your styles in `src/styles/<your-area>.css`. Never use a generic file name such as `Banner.astro` outside your folder.
4. Edit only the files that your brief lists. Do not edit `astro.config.mjs`, `package.json` or `pnpm-lock.yaml` unless your brief says so. Add no dependency.
5. Commit on your own branch. Do not push. Do not open a pull request.
