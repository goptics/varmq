# varmq site

varmq's story in ten steps: a page that snaps one step per scroll, plays that chapter on the map, and morphs one code
block into the code behind it. Plain HTML, CSS and TypeScript on Vite, with no framework.

```bash
pnpm install
pnpm dev           # http://localhost:5173
pnpm build         # dist/
pnpm test          # the map's and the stations' tests
pnpm types:check
```

- `index.html`, `src/`: the page. `src/main.ts` drives the steps, `src/map.ts` the map, and `src/steps.ts` builds
  each step from the map's chapters and the stations' code.
- `lib/heromap/`: the map, copied from `docs/src/lib/heromap`, plus the tag that names a Redis job's holder,
  chapters that settle a second before they end (the page stops on each one's last frame), no labels on the
  whole map, and all four queue strategies, which chapter 5 runs in turn.
- `lib/home/stations.ts`: each step's code, and the lines each caption lights. `lib/home/compile.ts` highlights it
  at build time (`vite.config.ts`), so the page ships tokens, not Shiki.
- `src/fonts/`: Go Sans and Go Mono, made by `make.py` from `golang.org/x/image`.
