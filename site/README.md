# VarMQ site

The next VarMQ docs site, built with [Fumadocs](https://fumadocs.dev) on Next.js and exported as static files for GitHub Pages. It replaces the Starlight site in `../docs`, which stays the published site until the content moves here.

```bash
pnpm install
pnpm dev     # http://localhost:3000
pnpm build   # static site in out/
pnpm start   # serve out/ as GitHub Pages would
```

- `content/docs`: the pages, in MDX. The `/docs` URL prefix is the template's default, not a decision; the migration decides whether the current root URLs, such as `/start/quick-start/`, stay.
- `lib/source.ts`: the content source; `lib/shared.ts`: the name and repository links.
- `app/(home)`: the home page. `app/docs`: the docs layout and pages. `app/api/search/route.ts`: the search index, built at export time and searched in the browser.
