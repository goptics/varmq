// The hero map, drawn: the world of `scene.ts` as SVG with every piece and line on, a goroutine ring at each
// place in RINGS, an element for every job and store record the chapters play (`chapters.ts`), and every caption
// as a line of words. It draws once and keeps no time: the choreographer (`choreo.ts`) shows, hides and moves what
// is here. Colours, type and the glow come from custom properties on `varmq-map` (`map.css`), so a theme change
// restyles these same nodes. The square has no background of its own: the page shows through, under a dot grid
// that moves with the camera and fades out toward the square's edges.
//
// Hooks for the choreographer:
// - `piece(id)`: a `<g id="vm-<id>">` for each piece and line, shown. Its outline is `.vm-shape` (a rect, the
//   loop's circle or a line's path), to draw in. The groups marked `.vm-dashed` (the trays and the ack lines)
//   fade in instead, since drawing would turn their dashes solid, and so do a lane's or tray's five
//   `.vm-slot`s. Text is `.vm-label` (the ACK line has one too), or `.vm-text` inside a chip. The loop also holds
//   `.vm-pulse`, a ring over its circle, hidden.
// - `piece('<id>@<n>')`: chapter n's transient chip in piece `<id>`'s place (`Chapter.chips`), its own element,
//   hidden. Chapter 2's TunePool(5) is `piece('ctl-restart@2')`.
// - `piece('s-prog')`, the SQLite program, also holds its crash, hidden: `.vm-flash`, a red outline over its
//   box, and the labels `.vm-died` (PROGRAM · DIED) and `.vm-restarted` (PROGRAM · RESTARTED) after its own
//   `.vm-label`. Redis's program B (`b-prog`) holds its `.vm-flash` and `.vm-died` (PROGRAM B · DIED), and comes
//   back as itself.
// - `ring(worker, i)`: for each place in `RINGS[worker]`, the ring (a group at scale 1, shown if the finale starts
//   with it and hidden otherwise, as the still frame has them), its progress arc (r 15, drawing clockwise from 12
//   o'clock) and its check, both hidden, and the dashed ghost under the ring, shown. The arc and the check sit
//   beside the ring's group, so a pulse of the ring leaves them be.
// - `job(id)`: [main, trail 1, trail 2] for every job and published value in the chapters, hidden at the world's
//   origin. Each is a lane slot's square centred on its own origin, so `x` and `y` place it. The trails' faintness
//   (.35 and .15) is on their square: show all three at opacity 1. A priority job's main square shows its number.
// - `record(id)`: each store job's record, the same square, hidden. In flight it is an outline: take its square's
//   `fill-opacity` to 0, and the stroke stays.
// - Hidden is the presentation attribute `opacity="0"`, which an inline `opacity` (what anime.js writes)
//   overrides. Nothing is `display: none`, which would break the lengths that drawing and motion paths need.
// - `--vm-label` holds `labelOpacity` at the scale on screen: the camera's zoom times the map's width on screen
//   over 400, kept by `setCamera` and `setWidth`. The stylesheet applies it to text as `fill-opacity`, so a
//   label's `opacity` stays free to animate. Move the view only through `setCamera`: it owns the world's
//   transform and the dot grid's.
// - Rings, jobs, records, chips, the pulse and the flash scale about their own centre (`transform-box: fill-box`).
//   Nothing here is placed with a transform, which an animated CSS transform would replace, except the progress
//   arcs' rotate(-90): leave their transform alone.
// - `words(n, i)`: chapter n's caption i (`Chapter.captions`) as its words, hidden (an inline `opacity: 0`). Each
//   is the inner element of a word's clipping box, to rise in. The lines are stacked in the caption band.
// - `swap(n, i)`: for a caption with a `swap`, the word that takes its last word's place: in the same box, stacked
//   on it, and hidden.

import { CHAPTERS } from './chapters.ts';
import {
	CONNS,
	LABEL_GAP,
	LABEL_PX,
	LANE,
	PIECES,
	RINGS,
	RING_R,
	SLOTS,
	VIEW,
	labelOpacity,
	slotX,
	slotY,
	type Camera,
	type Piece,
	type PieceKind,
} from './scene.ts';
import { workers } from './stores.ts';

export interface MapNodes {
	svg: SVGSVGElement;
	world: SVGGElement;
	caption: HTMLElement;
	/** Pieces and connectors by id, and transient chips by `<id>@<chapter>`. */
	piece(id: string): SVGGraphicsElement;
	ring(
		worker: string,
		i: number,
	): { ring: SVGGElement; progress: SVGCircleElement; check: SVGPathElement; ghost: SVGElement };
	/** [main, trail 1, trail 2] */
	job(id: string): SVGGElement[];
	/** A store record (SQLite and Redis chapters). */
	record(id: string): SVGGElement;
	/** Chapter n's caption i, as its words. */
	words(n: number, i: number): HTMLElement[];
	/** The word that takes the last word's place in chapter n's caption i, which must have a `swap`. */
	swap(n: number, i: number): HTMLElement;
	/** Transforms the world and sets `--vm-label` from `labelOpacity` at the scale on screen. */
	setCamera(c: Camera): void;
	/** The map's width on screen, in CSS px (400 until set): the zoom times it over 400 is the scale there. */
	setWidth(px: number): void;
}

type Ring = ReturnType<MapNodes['ring']>;
type Attrs = Record<string, string | number>;

const NS = 'http://www.w3.org/2000/svg';
const FEATHER = 16; // world units, within the 20 that `frame` (scene.ts) leaves around a chapter
const DESCENT = 3; // from the bottom of a 14-unit line up to its baseline
const ARC_R = 15; // a ring's progress arc, around RING_R
const RADIUS: Record<Exclude<PieceKind, 'loop'>, number> = {
	program: 16,
	store: 14,
	lane: 10,
	tray: 10,
	card: 14,
	chip: 14,
};
const TONE: Record<string, string> = { 'r-value': 'vm-value', 'r-error': 'vm-error' }; // the result worker's chips
// The stores' logos, which lead their labels: Simple Icons' glyphs (CC0), on a 24-unit grid.
const LOGOS: Record<string, { name: string; d: string }> = {
	's-store': {
		name: 'sqlite',
		d: 'M21.678.521c-1.032-.92-2.28-.55-3.513.544a8.71 8.71 0 0 0-.547.535c-2.109 2.237-4.066 6.38-4.674 9.544.237.48.422 1.093.544 1.561a13.044 13.044 0 0 1 .164.703s-.019-.071-.096-.296l-.05-.146a1.689 1.689 0 0 0-.033-.08c-.138-.32-.518-.995-.686-1.289-.143.423-.27.818-.376 1.176.484.884.778 2.4.778 2.4s-.025-.099-.147-.442c-.107-.303-.644-1.244-.772-1.464-.217.804-.304 1.346-.226 1.478.152.256.296.698.422 1.186.286 1.1.485 2.44.485 2.44l.017.224a22.41 22.41 0 0 0 .056 2.748c.095 1.146.273 2.13.5 2.657l.155-.084c-.334-1.038-.47-2.399-.41-3.967.09-2.398.642-5.29 1.661-8.304 1.723-4.55 4.113-8.201 6.3-9.945-1.993 1.8-4.692 7.63-5.5 9.788-.904 2.416-1.545 4.684-1.931 6.857.666-2.037 2.821-2.912 2.821-2.912s1.057-1.304 2.292-3.166c-.74.169-1.955.458-2.362.629-.6.251-.762.337-.762.337s1.945-1.184 3.613-1.72C21.695 7.9 24.195 2.767 21.678.521m-18.573.543A1.842 1.842 0 0 0 1.27 2.9v16.608a1.84 1.84 0 0 0 1.835 1.834h9.418a22.953 22.953 0 0 1-.052-2.707c-.006-.062-.011-.141-.016-.2a27.01 27.01 0 0 0-.473-2.378c-.121-.47-.275-.898-.369-1.057-.116-.197-.098-.31-.097-.432 0-.12.015-.245.037-.386a9.98 9.98 0 0 1 .234-1.045l.217-.028c-.017-.035-.014-.065-.031-.097l-.041-.381a32.8 32.8 0 0 1 .382-1.194l.2-.019c-.008-.016-.01-.038-.018-.053l-.043-.316c.63-3.28 2.587-7.443 4.8-9.791.066-.069.133-.128.198-.194Z',
	},
	'r-store': {
		name: 'redis',
		d: 'M22.71 13.145c-1.66 2.092-3.452 4.483-7.038 4.483-3.203 0-4.397-2.825-4.48-5.12.701 1.484 2.073 2.685 4.214 2.63 4.117-.133 6.94-3.852 6.94-7.239 0-4.05-3.022-6.972-8.268-6.972-3.752 0-8.4 1.428-11.455 3.685C2.59 6.937 3.885 9.958 4.35 9.626c2.648-1.904 4.748-3.13 6.784-3.744C8.12 9.244.886 17.05 0 18.425c.1 1.261 1.66 4.648 2.424 4.648.232 0 .431-.133.664-.365a100.49 100.49 0 0 0 5.54-6.765c.222 3.104 1.748 6.898 6.014 6.898 3.819 0 7.604-2.756 9.33-8.965.2-.764-.73-1.361-1.261-.73zm-4.349-5.013c0 1.959-1.926 2.922-3.685 2.922-.941 0-1.664-.247-2.235-.568 1.051-1.592 2.092-3.225 3.21-4.973 1.972.334 2.71 1.43 2.71 2.619z',
	},
};
const LOGO = 12; // a logo's size: the height of the label's capitals, and a little more
const LOGO_GAP = 5; // between a logo and its label's text
// The programs that die mid-job, and whether one comes back labelled RESTARTED: SQLite's in chapter 7 does, and
// Redis's program B in chapter 8 comes back as itself.
const CRASHES = new Map([
	['s-prog', true],
	['b-prog', false],
]);
const HIDDEN = { opacity: 0 };

function add<K extends keyof SVGElementTagNameMap>(
	parent: Element,
	tag: K,
	attrs: Attrs = {},
	content?: string,
): SVGElementTagNameMap[K] {
	const node = document.createElementNS(NS, tag);
	for (const [name, value] of Object.entries(attrs)) node.setAttribute(name, String(value));
	if (content !== undefined) node.textContent = content;
	parent.append(node);
	return node;
}

/**
 * The dot grid, its fade toward the square's edges, the glow and the lanes' edge, which brightens toward the
 * head. Returns the grid's pattern, which `setCamera` moves with the world.
 */
function defs(svg: SVGSVGElement): SVGPatternElement {
	const defs = add(svg, 'defs');
	const dots = add(defs, 'pattern', { id: 'vm-dots', width: 14, height: 14, patternUnits: 'userSpaceOnUse' });
	add(dots, 'circle', { class: 'vm-dot', cx: 1, cy: 1, r: 0.8 });
	// Full in the middle of the square and gone at its edges, so no edge shows against the page.
	const fade = add(defs, 'radialGradient', {
		id: 'vm-fade',
		cx: VIEW.w / 2,
		cy: VIEW.h / 2,
		r: VIEW.w / 2,
		gradientUnits: 'userSpaceOnUse',
	});
	add(fade, 'stop', { offset: 0.35, 'stop-color': '#fff' });
	add(fade, 'stop', { offset: 1, 'stop-color': '#fff', 'stop-opacity': 0 });
	const mask = add(defs, 'mask', { id: 'vm-grid-fade' });
	add(mask, 'rect', { width: VIEW.w, height: VIEW.h, fill: 'url(#vm-fade)' });
	const glow = add(defs, 'filter', { id: 'vm-glow', x: '-80%', y: '-80%', width: '260%', height: '260%' });
	add(glow, 'feGaussianBlur', { stdDeviation: 2.6, result: 'blur' });
	const merge = add(glow, 'feMerge');
	add(merge, 'feMergeNode', { in: 'blur' });
	add(merge, 'feMergeNode', { in: 'SourceGraphic' });
	const edge = add(defs, 'linearGradient', { id: 'vm-lane-edge', x1: 0, x2: 1 });
	add(edge, 'stop', { class: 'vm-edge-tail', offset: 0 });
	add(edge, 'stop', { class: 'vm-edge-head', offset: 1 });
	return dots;
}

/** A piece's group: its outline, a lane's slots, and its label (above it, below the loop, inside a chip). */
function drawPiece(world: SVGGElement, p: Piece, key = p.id, label = p.label, attrs: Attrs = {}): SVGGElement {
	const { x, y, w, h } = p.box;
	const classes = ['vm-piece', `vm-${p.kind}`, p.kind === 'tray' && 'vm-dashed', TONE[p.id]].filter(Boolean);
	const g = add(world, 'g', { id: `vm-${key}`, class: classes.join(' '), ...attrs });
	const top = y - LABEL_GAP - DESCENT; // the baseline of a label above the piece

	if (p.kind === 'loop') {
		add(g, 'circle', { class: 'vm-shape', cx: x + w / 2, cy: y + h / 2, r: w / 2 });
		add(g, 'circle', { class: 'vm-pulse', cx: x + w / 2, cy: y + h / 2, r: w / 2, ...HIDDEN });
	} else {
		const outline = { x, y, width: w, height: h, rx: RADIUS[p.kind] };
		add(g, 'rect', { class: 'vm-shape', ...outline });
		if (CRASHES.has(p.id)) add(g, 'rect', { class: 'vm-flash', ...outline, ...HIDDEN });
	}
	if (p.kind === 'lane' || p.kind === 'tray') {
		for (let k = 0; k < SLOTS; k++) {
			const half = LANE.slot / 2;
			add(g, 'rect', {
				class: 'vm-slot',
				x: slotX(x, k) - half,
				y: slotY(y) - half,
				width: LANE.slot,
				height: LANE.slot,
				rx: 5,
			});
		}
	}

	if (label === undefined) return g;
	if (p.kind === 'chip') {
		const centre = { x: x + w / 2, y: y + h / 2, 'text-anchor': 'middle', 'dominant-baseline': 'central' };
		add(g, 'text', { class: 'vm-text', ...centre }, label);
	} else if (p.kind === 'loop') {
		// EVENT over LOOP: on one line it would reach the worker's card and the third queue's line.
		const baseline = y + h + LABEL_GAP + LABEL_PX - DESCENT;
		const text = add(g, 'text', { class: 'vm-label', 'text-anchor': 'middle' });
		label.split('\n').forEach((line, i) => add(text, 'tspan', { x: x + w / 2, y: baseline + LABEL_PX * i }, line));
	} else {
		const logo = LOGOS[p.id];
		if (logo) {
			const at = `translate(${x} ${top - LOGO + 1}) scale(${LOGO / 24})`;
			add(g, 'path', { class: `vm-logo vm-logo-${logo.name}`, d: logo.d, transform: at });
		}
		add(g, 'text', { class: 'vm-label', x: logo ? x + LOGO + LOGO_GAP : x, y: top }, label);
	}
	if (CRASHES.has(p.id)) {
		add(g, 'text', { class: 'vm-label vm-died', x, y: top, ...HIDDEN }, `${label} · DIED`);
		if (CRASHES.get(p.id)) add(g, 'text', { class: 'vm-label vm-restarted', x, y: top, ...HIDDEN }, `${label} · RESTARTED`);
	}
	return g;
}

/**
 * A caption line, one clipping box (`.vm-w`) per word. `*marked*` text is green (`em`), `!marked!` text red (`b`)
 * and `#marked#` text in its logo's colour (`.vm-brand-<name>`), as `Caption` in chapters.ts marks it; a mark may
 * wrap several words. Returns the line and each word's
 * inner element, hidden, which is what rises in.
 */
function captionLine(text: string, swap?: string): { line: HTMLElement; words: HTMLElement[]; swap?: HTMLElement } {
	const line = document.createElement('span');
	line.className = 'vm-cap';
	const words: HTMLElement[] = [];
	let mark = ''; // the open mark, `*` or `!`, carried from word to word
	let box = line;
	for (const token of text.split(' ')) {
		const made = captionWord(token, mark);
		mark = made.mark;
		box = document.createElement('span');
		box.className = 'vm-w';
		box.append(made.word);
		if (words.length > 0) line.append(' ');
		line.append(box);
		words.push(made.word);
	}
	if (swap === undefined) return { line, words };
	// The word that takes the last one's place shares its box, stacked on it.
	const next = captionWord(swap, '').word;
	box.classList.add('vm-swap');
	box.append(next);
	return { line, words, swap: next };
}

/** A caption word, hidden, from its token; `open` is the mark open before it, and `mark` the one open after it. */
function captionWord(token: string, open: string): { word: HTMLElement; mark: string } {
	const word = document.createElement('span');
	word.style.opacity = '0';
	let mark = open;
	for (const part of token.split(/([*!#])/)) {
		if (part === '*' || part === '!' || part === '#') {
			mark = mark === part ? '' : mark || part;
		} else if (part && mark) {
			const run = document.createElement(mark === '*' ? 'em' : mark === '!' ? 'b' : 'span');
			if (mark === '#') run.className = `vm-brand-${part.toLowerCase().replace(/\W/g, '')}`;
			run.textContent = part;
			word.append(run);
		} else if (part) {
			word.append(part);
		}
	}
	return { word, mark };
}

/** A job or record: a lane slot's square centred on the group's origin, which the choreographer moves. */
function square(world: SVGGElement, attrs: Attrs): SVGGElement {
	const g = add(world, 'g', { ...attrs, ...HIDDEN });
	const half = LANE.slot / 2;
	add(g, 'rect', { class: 'vm-jb', x: -half, y: -half, width: LANE.slot, height: LANE.slot, rx: 5 });
	return g;
}

/** Every job and published value the chapters play, with a priority job's number, and the store jobs. */
function played() {
	const jobs = new Map<string, number | undefined>();
	const records = new Set<string>();
	for (const chapter of CHAPTERS) {
		for (const value of chapter.published ?? []) jobs.set(value.id, undefined);
		for (const part of chapter.parts) {
			if (part.kind === 'pool') {
				for (const job of part.plan.jobs) jobs.set(job.id, job.priority);
				continue;
			}
			for (const job of part.plan.jobs) {
				jobs.set(job.id, undefined);
				records.add(job.id);
			}
		}
	}
	return { jobs, records };
}

/**
 * The rings the finale starts with, as `<worker>:<place>`: each worker's idle goroutines, as its last chapter left
 * them. The still frame shows these, with a ghost at every other place.
 */
function finaleRings(): Set<string> {
	const finale = CHAPTERS[CHAPTERS.length - 1];
	return new Set(
		finale.parts.flatMap((part) => {
			if (part.kind === 'pool') return part.plan.idle.map((i) => `${part.worker}:${i}`);
			return workers(part.plan).flatMap((w) => w.idle.map((i) => `${w.id === 'sqlite' ? w.id : `redis-${w.id}`}:${i}`));
		}),
	);
}

function lookup<T>(map: Map<string, T>, what: string) {
	return (key: string): T => {
		const found = map.get(key);
		if (found === undefined) throw new Error(`heromap: no ${what} ${key}`);
		return found;
	};
}

export function renderMap(host: HTMLElement): MapNodes {
	const svg = document.createElementNS(NS, 'svg');
	svg.setAttribute('class', 'vm-svg');
	svg.setAttribute('viewBox', `0 0 ${VIEW.w} ${VIEW.h}`);
	const dots = defs(svg);
	// The grid covers the square and stays put, under the world. Its pattern moves with the camera, and so its
	// dots move with the world.
	const grid = { class: 'vm-grid', width: VIEW.w, height: VIEW.h, mask: 'url(#vm-grid-fade)' };
	add(svg, 'rect', { ...grid, fill: 'url(#vm-dots)' });
	const world = add(svg, 'g', { class: 'vm-world' });

	const pieces = new Map<string, SVGGraphicsElement>();
	for (const c of CONNS) {
		const classes = ['vm-conn', `vm-${c.kind}`, c.kind !== 'line' && 'vm-dashed'].filter(Boolean);
		const g = add(world, 'g', { id: `vm-${c.id}`, class: classes.join(' ') });
		add(g, 'path', { class: 'vm-shape', d: c.d });
		if (c.label) add(g, 'text', { class: 'vm-label', x: c.label.x, y: c.label.y - DESCENT }, c.label.text);
		pieces.set(c.id, g);
	}
	for (const p of PIECES) pieces.set(p.id, drawPiece(world, p));
	for (const chapter of CHAPTERS) {
		for (const chip of chapter.chips ?? []) {
			const key = `${chip.id}@${chapter.n}`;
			const p = PIECES.find((q) => q.id === chip.id);
			if (!p) throw new Error(`heromap: chapter ${chapter.n} has a chip at no piece ${chip.id}`);
			pieces.set(key, drawPiece(world, p, key, chip.label, HIDDEN));
		}
	}

	const rings = new Map<string, Ring>();
	const still = finaleRings();
	for (const [worker, places] of Object.entries(RINGS)) {
		places.forEach(({ x, y }, i) => {
			const data = { 'data-ring': `${worker}:${i}` };
			const ghost = add(world, 'circle', { class: 'vm-ghost', cx: x, cy: y, r: RING_R, ...data });
			const ring = add(world, 'g', { class: 'vm-go', ...data, ...(still.has(`${worker}:${i}`) ? {} : HIDDEN) });
			add(ring, 'circle', { class: 'vm-ring', cx: x, cy: y, r: RING_R });
			const progress = add(world, 'circle', {
				class: 'vm-progress',
				cx: x,
				cy: y,
				r: ARC_R,
				transform: `rotate(-90 ${x} ${y})`,
				...data,
				...HIDDEN,
			});
			const tick = `M${x - 4.7} ${y}l3.1 3.5 6.3-7`;
			const check = add(world, 'path', { class: 'vm-check', d: tick, ...data, ...HIDDEN });
			rings.set(`${worker}:${i}`, { ring, progress, check, ghost });
		});
	}

	// Records under the jobs, which fly over everything.
	const { jobs: ids, records: recordIds } = played();
	const records = new Map<string, SVGGElement>();
	for (const id of recordIds) records.set(id, square(world, { class: 'vm-record', 'data-record': id }));
	const jobs = new Map<string, SVGGElement[]>();
	for (const [id, priority] of ids) {
		const t2 = square(world, { class: 'vm-job vm-trail vm-t2', 'data-job': id });
		const t1 = square(world, { class: 'vm-job vm-trail vm-t1', 'data-job': id });
		const main = square(world, { class: 'vm-job vm-main', 'data-job': id });
		if (priority !== undefined) {
			const centre = { x: 0, y: 0, 'text-anchor': 'middle', 'dominant-baseline': 'central' };
			add(main, 'text', { class: 'vm-num', ...centre }, String(priority));
		}
		jobs.set(id, [main, t1, t2]);
	}

	const caption = document.createElement('div');
	caption.className = 'vm-caption';
	caption.setAttribute('aria-hidden', 'true');
	const lines = new Map<string, HTMLElement[]>();
	const swaps = new Map<string, HTMLElement>();
	for (const chapter of CHAPTERS) {
		chapter.captions.forEach(({ text, swap }, i) => {
			const made = captionLine(text, swap?.text);
			caption.append(made.line);
			lines.set(`${chapter.n}:${i}`, made.words);
			if (made.swap) swaps.set(`${chapter.n}:${i}`, made.swap);
		});
	}
	host.append(svg, caption);

	const findRing = lookup(rings, 'ring');
	const findLine = lookup(lines, 'caption');
	const findSwap = lookup(swaps, 'caption swap');
	let zoom = Number.NaN; // the camera's, until the first setCamera
	let width: number = VIEW.w; // the map's on screen
	let shown = Number.NaN; // what --vm-label holds: most camera moves leave it as it is
	// Labels show by their size on screen, so a smaller square hides them sooner.
	const legible = () => {
		const label = labelOpacity((zoom * width) / VIEW.w);
		if (Number.isNaN(label) || label === shown) return;
		world.style.setProperty('--vm-label', String(label));
		shown = label;
	};
	return {
		svg,
		world,
		caption,
		piece: lookup(pieces, 'piece'),
		ring: (worker, i) => findRing(`${worker}:${i}`),
		job: lookup(jobs, 'job'),
		record: lookup(records, 'record'),
		words: (n, i) => findLine(`${n}:${i}`),
		swap: (n, i) => findSwap(`${n}:${i}`),
		setCamera({ x, y, s }) {
			// (x, y) goes to the middle of the map area, the square above the caption band.
			const dx = VIEW.w / 2 - x * s;
			const dy = VIEW.mapH / 2 - y * s;
			const transform = `translate(${dx} ${dy}) scale(${s})`;
			world.setAttribute('transform', transform);
			dots.setAttribute('patternTransform', transform);
			// The map fades into the page FEATHER world units deep at the square's edges (map.css): inside the margin
			// every chapter's frame leaves, so only what the edge cuts fades.
			if (s !== zoom) svg.style.setProperty('--vm-feather', `${(FEATHER * s * 100) / VIEW.w}%`);
			zoom = s;
			legible();
		},
		setWidth(px) {
			width = px;
			legible();
		},
	};
}
