// The hero map, drawn: the world of `scene.ts` as SVG with every piece and line on, a goroutine ring at each
// place in RINGS, and an element for every job and store record the chapters play (`chapters.ts`). It draws once
// and keeps no time: the choreographer (`choreo.ts`) shows, hides and moves what is here. Colours, type and the
// glow come from custom properties on `varmq-map` (HeroMap.astro), so a theme change restyles these same nodes.
//
// Hooks for the choreographer:
// - `piece(id)`: a `<g id="vm-<id>">` for each piece and line, shown. Its outline is `.vm-shape` (a rect, the
//   loop's circle or a line's path), to draw in. The groups marked `.vm-dashed` (the trays, the notify and ack
//   lines) fade in instead, since drawing would turn their dashes solid, and so do a lane's or tray's five
//   `.vm-slot`s. Text is `.vm-label` (the ACK line has one too), or `.vm-text` inside a chip. The loop also holds
//   `.vm-pulse`, a ring over its circle, hidden.
// - `piece('<id>@<n>')`: chapter n's transient chip in piece `<id>`'s place (`Chapter.chips`), its own element,
//   hidden. Chapter 2's TunePool(3) is `piece('ctl-tune@2')`.
// - `piece('s-prog')`, the SQLite program, also holds its crash, hidden: `.vm-flash`, a red outline over its
//   box, and the labels `.vm-died` (PROGRAM · DIED) and `.vm-restarted` (PROGRAM · RESTARTED) after its own
//   `.vm-label`.
// - `ring(worker, i)`: for each place in `RINGS[worker]`, the ring (a group, shown at scale 1), its progress arc
//   (r 15, drawing clockwise from 12 o'clock) and its check, both hidden, and the dashed ghost under the ring,
//   shown. The arc and the check sit beside the ring's group, so a pulse of the ring leaves them be.
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
//   transform.
// - Rings, jobs, records, chips, the pulse and the flash scale about their own centre (`transform-box: fill-box`).
//   Nothing here is placed with a transform, which an animated CSS transform would replace, except the progress
//   arcs' rotate(-90): leave their transform alone.

import { CHAPTERS } from './chapters.ts';
import {
	CONNS,
	LABEL_PX,
	LANE,
	PIECES,
	RINGS,
	VIEW,
	labelOpacity,
	slotX,
	slotY,
	type Camera,
	type Piece,
	type PieceKind,
} from './scene.ts';

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
	/** Transforms the world and sets `--vm-label` from `labelOpacity` at the scale on screen. */
	setCamera(c: Camera): void;
	/** The map's width on screen, in CSS px (400 until set): the zoom times it over 400 is the scale there. */
	setWidth(px: number): void;
}

type Ring = ReturnType<MapNodes['ring']>;
type Attrs = Record<string, string | number>;

const NS = 'http://www.w3.org/2000/svg';
const SLOTS = 5; // a lane's slots (LANE in scene.ts)
const LABEL_GAP = 6; // between a label and the piece it names (`Piece` in scene.ts)
const DESCENT = 3; // from the bottom of a 14-unit line up to its baseline
const RING_R = 11; // goroutine rings are 32 apart in the core's card,
const ARC_R = 15; // so a ring and its progress arc stay this small
const RADIUS: Record<Exclude<PieceKind, 'loop'>, number> = {
	program: 16,
	store: 14,
	lane: 10,
	tray: 10,
	card: 14,
	chip: 14,
};
const TONE: Record<string, string> = { 'r-value': 'vm-value', 'r-error': 'vm-error' }; // the result worker's chips
const CRASHES = 's-prog'; // the SQLite program, which dies and restarts in chapter 7
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

/** The dot grid, the glow and the lanes' edge, which brightens toward the head. */
function defs(svg: SVGSVGElement) {
	const defs = add(svg, 'defs');
	const dots = add(defs, 'pattern', { id: 'vm-dots', width: 14, height: 14, patternUnits: 'userSpaceOnUse' });
	add(dots, 'circle', { class: 'vm-dot', cx: 1, cy: 1, r: 0.8 });
	const glow = add(defs, 'filter', { id: 'vm-glow', x: '-80%', y: '-80%', width: '260%', height: '260%' });
	add(glow, 'feGaussianBlur', { stdDeviation: 2.6, result: 'blur' });
	const merge = add(glow, 'feMerge');
	add(merge, 'feMergeNode', { in: 'blur' });
	add(merge, 'feMergeNode', { in: 'SourceGraphic' });
	const edge = add(defs, 'linearGradient', { id: 'vm-lane-edge', x1: 0, x2: 1 });
	add(edge, 'stop', { class: 'vm-edge-tail', offset: 0 });
	add(edge, 'stop', { class: 'vm-edge-head', offset: 1 });
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
		if (p.id === CRASHES) add(g, 'rect', { class: 'vm-flash', ...outline, ...HIDDEN });
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
		const baseline = y + h + LABEL_GAP + LABEL_PX - DESCENT;
		add(g, 'text', { class: 'vm-label', x: x + w / 2, y: baseline, 'text-anchor': 'middle' }, label);
	} else {
		add(g, 'text', { class: 'vm-label', x, y: top }, label);
	}
	if (p.id === CRASHES) {
		add(g, 'text', { class: 'vm-label vm-died', x, y: top, ...HIDDEN }, 'PROGRAM · DIED');
		add(g, 'text', { class: 'vm-label vm-restarted', x, y: top, ...HIDDEN }, 'PROGRAM · RESTARTED');
	}
	return g;
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
	defs(svg);
	const world = add(svg, 'g', { class: 'vm-world' });
	// Wide enough to fill the square at the finale's zoom, wherever the camera stops.
	add(world, 'rect', { class: 'vm-grid', x: -1000, y: -1000, width: 3400, height: 3000, fill: 'url(#vm-dots)' });

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
	for (const [worker, places] of Object.entries(RINGS)) {
		places.forEach(({ x, y }, i) => {
			const data = { 'data-ring': `${worker}:${i}` };
			const ghost = add(world, 'circle', { class: 'vm-ghost', cx: x, cy: y, r: RING_R, ...data });
			const ring = add(world, 'g', { class: 'vm-go', ...data });
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
	host.append(svg, caption);

	const findRing = lookup(rings, 'ring');
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
		setCamera({ x, y, s }) {
			// (x, y) goes to the middle of the map area, the square above the caption band.
			const dx = VIEW.w / 2 - x * s;
			const dy = VIEW.mapH / 2 - y * s;
			world.setAttribute('transform', `translate(${dx} ${dy}) scale(${s})`);
			zoom = s;
			legible();
		},
		setWidth(px) {
			width = px;
			legible();
		},
	};
}
