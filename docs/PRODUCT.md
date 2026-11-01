# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Go developers who need to run work concurrently: background jobs, fan-out, retries, work that must survive a restart. Two situations, both primary:

- **Evaluating.** Comparing varmq with other Go worker pools and job queues before choosing one. They read the introduction, the comparison and the benchmarks, and want to see how it behaves before they write code.
- **Using.** Already integrating varmq. They come for a specific guide (concurrency, errors, waiting, persistence), copy a program, and leave.

## Product Purpose

The docs site for varmq (github.com/goptics/varmq), a Go library of worker pools and job queues. It teaches the API by showing it: each claim on a page sits next to a small working scene and the Go call that produces it. Success is a reader who understands what a call does before running it, and who can pick in-memory, Redis or SQLite storage without reading the source.

## Positioning

Storage-agnostic with one API: the same worker and queue calls run whether the jobs live in memory, in Redis or in SQLite, and other stores plug in through small adapter interfaces. Persistent and distributed queues are the same worker with a different queue bound to it.

## Operating Context

- Readers arrive from pkg.go.dev, GitHub and search, often on a desktop next to an editor, sometimes on a phone.
- Every example program on the site is real (docs/examples/*/main.go), compiled and tested in CI, and can run in the Go Playground from the page.
- Benchmark figures come from github.com/goptics/benchmarks; the site reads its raw output (src/components/bench/data.json).

## Capabilities and Constraints

- Static site: Astro with Fumadocs (@fumadocs/astro), deployed to GitHub Pages (varmq.goptics.org), with a Cloudflare Workers preview per pull request. Output is dist/.
- Pages are MDX under src/content/docs; the interactive scenes are Astro components with their own scripts, and must keep working on every page they are on.
- The home page `/` is a landing page with a scroll-driven tour (the story) and an animated map.
- Search is Fumadocs' static index; navigation does full page loads.
- An API-coverage check fails the build when an exported identifier is never mentioned in the docs.

## Brand Commitments

- The mascot logo (assets/logo-light.svg, logo-dark.svg) and its green (#2e9c7f light, #44bf9e dark). The palette derives from it; no colour comes from outside the mascot and the scenes.
- Every interactive visualization and motion graphic stays, working and visible.
- Fonts: Big Shoulders Display (headlines), IBM Plex Sans (text), JetBrains Mono (code, labels, scenes).
- Light, dark and System themes are all first-class.
- The idea of the visual system (docs/DESIGN.md): the queue is a roll of paper, a page is a print job (`JOB nn`), one square is one job, one lane is one worker.

## Evidence on Hand

- Benchmark data: src/components/bench/data.json (from goptics/benchmarks), shown on /reference/benchmarks/.
- 37 runnable example programs: docs/examples/.
- A feature comparison with other Go pools: /reference/comparison/.
- No testimonials, customer logos, adoption figures or press exist; future work must not invent them.

## Product Principles

1. Show, then tell: a claim earns a working scene beside the Go call that makes it.
2. One API, many stores: present storage as a choice made at bind time, never as a different product.
3. Every number is sourced: figures link back to the benchmark run or the godoc they come from.
4. Readers come to do something: a guide gets them to a correct program fast; the tour is optional.

## Accessibility & Inclusion

Each scene has a text alternative and a live region; motion stops under reduced motion; touch targets are at least 44 px on a coarse pointer; text meets WCAG AA contrast in both themes (docs/DESIGN.md).
