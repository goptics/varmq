// The hero map's world: where every piece sits, the lines between them, the goroutine rings and the
// camera stop for each chapter. Pure data and geometry, in world units, for the renderer and the
// choreographer to build on. The SQLite and Redis areas follow the `stores.html` mockup, moved by
// (+20, +540) and (+635, +540).

export type Box = { x: number; y: number; w: number; h: number };
/** The world point at the middle of the map area, and the scale. */
export type Camera = { x: number; y: number; s: number };

/** The square on the page. The map fills the top `mapH`; the bottom 56 px hold the caption. */
export const VIEW = { w: 400, h: 400, mapH: 344 } as const;
/** A queue lane: five 22 px slots 10 px apart, 10 px of padding on both sides, the head on the right. */
export const LANE = { w: 170, h: 44, slot: 22, step: 32, pad: 10 } as const;
/** How tall a label is, in world units. */
export const LABEL_PX = 14;

const MARGIN = 20; // what `frame` leaves around a box on every side
const MAX_ZOOM = 1.1;
const LABEL_HIDDEN_PX = 7; // a label this small on screen is hidden...
const LABEL_SHOWN_PX = 8.5; // ...and one this big is fully shown
const LABEL_GAP = 6; // between a label and the piece it names

/** Centre of slot `k` in the lane at `laneX`. Slot 0 is the head, on the right. */
export function slotX(laneX: number, k: number): number {
	return laneX + LANE.w - LANE.pad - LANE.slot / 2 - LANE.step * k;
}

/** The height the slots' centres sit at, in the lane at `laneY`. */
export function slotY(laneY: number): number {
	return laneY + LANE.h / 2;
}

export type PieceKind = 'program' | 'store' | 'lane' | 'tray' | 'card' | 'loop' | 'chip';

/**
 * Something drawn on the map. It switches on in `chapter` (1 to 9) and stays on. `parent` names the
 * piece whose box it sits inside. `label` is its text: inside a chip, 6 units below the loop, and 6
 * units above the top-left corner of anything else.
 */
export interface Piece {
	id: string;
	kind: PieceKind;
	box: Box;
	chapter: number;
	parent?: string;
	label?: string;
}

/** A line between two pieces, as SVG path data drawn the way its traffic goes: jobs, notifications or acks. */
export interface Conn {
	id: string;
	d: string;
	chapter: number;
	kind: 'line' | 'notify' | 'ack';
}

const rect = (x: number, y: number, w: number, h: number): Box => ({ x, y, w, h });
const lane = (x: number, y: number): Box => rect(x, y, LANE.w, LANE.h);
const chip = (x: number, cy: number, w: number): Box => rect(x, cy - 14, w, 28); // 28 high, centred on cy

// Drawn in this order, so a piece comes after the one it sits inside. Chapter 2 adds no piece: the pool
// grows inside the core's card.
export const PIECES: Piece[] = [
	// 1. One worker, left to right along y 390: Add(), the queue, the event loop and the worker.
	{ id: 'add', kind: 'chip', chapter: 1, box: chip(96, 390, 68), label: 'Add()' },
	{ id: 'queue', kind: 'lane', chapter: 1, box: lane(190, 368), label: 'QUEUE' },
	// The loop is a circle of radius 26 centred on (520, 390); its box is the square around it.
	{ id: 'loop', kind: 'loop', chapter: 1, box: rect(494, 364, 52, 52), label: 'LOOP' },
	{ id: 'core', kind: 'card', chapter: 1, box: rect(570, 340, 200, 100), label: 'WORKER' },

	// 3. Results and errors: a worker of its own, because a result worker cannot subscribe to a topic.
	{ id: 'r-add', kind: 'chip', chapter: 3, box: chip(840, 390, 68), label: 'Add()' },
	{ id: 'r-queue', kind: 'lane', chapter: 3, box: lane(932, 368), label: 'QUEUE' },
	{ id: 'result', kind: 'card', chapter: 3, box: rect(1126, 355, 130, 70), label: 'RESULT WORKER' },
	{ id: 'r-value', kind: 'chip', chapter: 3, box: chip(1280, 354, 60), label: 'value' },
	{ id: 'r-error', kind: 'chip', chapter: 3, box: chip(1280, 426, 60), label: 'error' },

	// 4. Priority, above the main row.
	{ id: 'prio', kind: 'lane', chapter: 4, box: lane(190, 290), label: 'PRIORITY QUEUE' },

	// 5. Many queues: a third lane below the main row, and the strategy chip on the line into the loop.
	{ id: 'queue-c', kind: 'lane', chapter: 5, box: lane(190, 446), label: 'QUEUE' },
	{ id: 'strategy', kind: 'chip', chapter: 5, box: chip(380, 390, 92), label: 'RoundRobin' },

	// 6. Topics: Publish() fills the topic lane, which feeds the core's loop and a second worker.
	{ id: 'publish', kind: 'chip', chapter: 6, box: chip(79, 180, 85), label: 'Publish()' },
	{ id: 'topic', kind: 'lane', chapter: 6, box: lane(190, 158), label: 'TOPIC' },
	{ id: 'second', kind: 'card', chapter: 6, box: rect(570, 145, 130, 70), label: 'ANOTHER WORKER' },

	// 7. SQLite, below the core: a program with Add() and a worker, and the jobs.db file with its lane and
	// tray. The store is 162 high, like Redis's, so the area ends at y 924.
	{ id: 's-prog', kind: 'program', chapter: 7, box: rect(60, 580, 520, 140), label: 'PROGRAM' },
	{ id: 's-add', kind: 'chip', chapter: 7, box: chip(88, 650, 68), label: 'Add()', parent: 's-prog' },
	{ id: 'sqlite', kind: 'card', chapter: 7, box: rect(360, 610, 200, 80), label: 'WORKER', parent: 's-prog' },
	{ id: 's-store', kind: 'store', chapter: 7, box: rect(220, 762, 210, 162), label: 'JOBS.DB · SQLITE' },
	{ id: 's-queue', kind: 'lane', chapter: 7, box: lane(240, 790), label: 'QUEUE', parent: 's-store' },
	{ id: 's-tray', kind: 'tray', chapter: 7, box: lane(240, 864), label: 'IN FLIGHT', parent: 's-store' },

	// 8. Redis, below the core: program A adds, the store holds the queue and the tray, B and C run workers.
	// Program A is 20 wider than in the mockup, so its label fits.
	{ id: 'a-prog', kind: 'program', chapter: 8, box: rect(650, 690, 170, 80), label: 'PROGRAM A · ADDS' },
	{ id: 'a-add', kind: 'chip', chapter: 8, box: chip(701, 730, 68), label: 'Add()', parent: 'a-prog' },
	{ id: 'r-store', kind: 'store', chapter: 8, box: rect(870, 652, 210, 162), label: 'REDIS' },
	{ id: 'r-queue2', kind: 'lane', chapter: 8, box: lane(890, 680), label: 'QUEUE', parent: 'r-store' },
	{ id: 'r-tray', kind: 'tray', chapter: 8, box: lane(890, 754), label: 'IN FLIGHT', parent: 'r-store' },
	{ id: 'b-prog', kind: 'program', chapter: 8, box: rect(1115, 590, 165, 120), label: 'PROGRAM B' },
	{ id: 'redis-b', kind: 'card', chapter: 8, box: rect(1130, 625, 135, 70), label: 'WORKER', parent: 'b-prog' },
	{ id: 'c-prog', kind: 'program', chapter: 8, box: rect(1115, 760, 165, 120), label: 'PROGRAM C' },
	{ id: 'redis-c', kind: 'card', chapter: 8, box: rect(1130, 795, 135, 70), label: 'WORKER', parent: 'c-prog' },

	// 9. Control: a row above the core's card.
	{ id: 'ctl-pause', kind: 'chip', chapter: 9, box: chip(570, 282, 70), label: 'Pause()' },
	{ id: 'ctl-resume', kind: 'chip', chapter: 9, box: chip(648, 282, 78), label: 'Resume()' },
	{ id: 'ctl-tune', kind: 'chip', chapter: 9, box: chip(734, 282, 100), label: 'TunePool(5)' },
	{ id: 'ctl-stop', kind: 'chip', chapter: 9, box: chip(842, 282, 64), label: 'Stop()' },
];

// Each line stops 2 units short of the pieces it joins. Where lines split or join, the shorter one runs
// only as far as the other, so no stretch is drawn twice. The two notify lines are the exception: each
// runs the whole way from the store.
export const CONNS: Conn[] = [
	// The core. Every queue's line ends at the event loop, the topic's included.
	{ id: 'add-to-queue', kind: 'line', chapter: 1, d: 'M166 390H188' },
	{ id: 'queue-to-loop', kind: 'line', chapter: 1, d: 'M362 390H492' },
	{ id: 'loop-to-core', kind: 'line', chapter: 1, d: 'M548 390H568' },
	{ id: 'prio-to-loop', kind: 'line', chapter: 4, d: 'M362 312H430L492 384' },
	{ id: 'queue-c-to-loop', kind: 'line', chapter: 5, d: 'M362 468H430L492 396' },
	{ id: 'publish-to-topic', kind: 'line', chapter: 6, d: 'M166 180H188' },
	{ id: 'topic-to-second', kind: 'line', chapter: 6, d: 'M362 172H568' },
	{ id: 'topic-to-loop', kind: 'line', chapter: 6, d: 'M362 188H520V362' },

	// The results cluster, drawn without a loop.
	{ id: 'r-add-to-r-queue', kind: 'line', chapter: 3, d: 'M910 390H930' },
	{ id: 'r-queue-to-result', kind: 'line', chapter: 3, d: 'M1104 390H1124' },
	{ id: 'result-to-r-value', kind: 'line', chapter: 3, d: 'M1258 382L1278 354' },
	{ id: 'result-to-r-error', kind: 'line', chapter: 3, d: 'M1258 398L1278 426' },

	// SQLite. Add() writes to the queue, the worker takes from its head, and its ack goes to the tray.
	{ id: 's-add-to-s-queue', kind: 'line', chapter: 7, d: 'M122 666V812H238' },
	{ id: 's-queue-to-sqlite', kind: 'line', chapter: 7, d: 'M412 812H460V692' },
	{ id: 'sqlite-to-s-tray', kind: 'ack', chapter: 7, d: 'M540 692V886H412' },

	// Redis. Program A adds, Redis notifies B and C, each takes from the queue and acks to the tray.
	{ id: 'a-add-to-r-queue2', kind: 'line', chapter: 8, d: 'M771 730H867V702H888' },
	{ id: 'r-queue2-to-redis-b', kind: 'line', chapter: 8, d: 'M1062 702H1095V660H1128' },
	{ id: 'r-queue2-to-redis-c', kind: 'line', chapter: 8, d: 'M1095 702V830H1128' },
	{ id: 'r-store-to-redis-b', kind: 'notify', chapter: 8, d: 'M1082 672H1105V640H1128' },
	{ id: 'r-store-to-redis-c', kind: 'notify', chapter: 8, d: 'M1082 672H1105V815H1128' },
	{ id: 'redis-b-to-r-tray', kind: 'ack', chapter: 8, d: 'M1128 680H1087V776H1062' },
	{ id: 'redis-c-to-r-tray', kind: 'ack', chapter: 8, d: 'M1128 845H1087V776' },
];

/**
 * Where each worker's goroutine rings sit, in the order the pool fills them. The core's grow outward
 * from the middle of its card, one lane step apart; the others sit centred in their card. `second` and
 * `result` run at concurrency 1, so each has one ring.
 */
export const RINGS: Record<
	'core' | 'result' | 'second' | 'sqlite' | 'redis-b' | 'redis-c',
	{ x: number; y: number }[]
> = {
	core: [670, 638, 702, 606, 734].map((x) => ({ x, y: 390 })),
	result: [{ x: 1191, y: 390 }],
	second: [{ x: 635, y: 180 }],
	sqlite: [
		{ x: 435, y: 650 },
		{ x: 485, y: 650 },
	],
	'redis-b': [
		{ x: 1172, y: 660 },
		{ x: 1222, y: 660 },
	],
	'redis-c': [
		{ x: 1172, y: 830 },
		{ x: 1222, y: 830 },
	],
};

const byId = new Map(PIECES.map((p) => [p.id, p]));

const union = (boxes: Box[]): Box => {
	const x = Math.min(...boxes.map((b) => b.x));
	const y = Math.min(...boxes.map((b) => b.y));
	return {
		x,
		y,
		w: Math.max(...boxes.map((b) => b.x + b.w)) - x,
		h: Math.max(...boxes.map((b) => b.y + b.h)) - y,
	};
};

/** What the camera has to keep in view for these pieces: their boxes and the labels above them. */
const around = (...ids: string[]): Box =>
	union(
		ids.map((id) => {
			const p = byId.get(id);
			if (!p) throw new Error(`unknown piece ${id}`);
			// The loop's label sits below it, inside the span of the core's card.
			if (!p.label || p.kind === 'chip' || p.kind === 'loop') return p.box;
			return rect(p.box.x, p.box.y - LABEL_PX - LABEL_GAP, p.box.w, p.box.h + LABEL_PX + LABEL_GAP);
		}),
	);

/** `REGIONS[n - 1]` is what the camera frames in chapter n. The last one is the whole world. */
export const REGIONS: Box[] = [
	around('add', 'queue', 'loop', 'core'), // 1. One worker
	around('queue', 'loop', 'core', 'ctl-tune'), // 2. The pool, with room for the TunePool(3) chip above the card
	around('r-add', 'r-queue', 'result', 'r-value', 'r-error'), // 3. Results and errors
	around('prio', 'loop', 'core'), // 4. Priority
	around('prio', 'queue-c', 'strategy', 'loop', 'core'), // 5. Many queues, one worker
	around('publish', 'topic', 'second', 'core'), // 6. Topics
	around('s-prog', 's-store'), // 7. SQLite
	around('a-prog', 'b-prog', 'c-prog', 'r-store'), // 8. Redis
	around('queue', 'loop', 'core', 'ctl-pause', 'ctl-resume', 'ctl-tune', 'ctl-stop'), // 9. Control
	union(PIECES.map((p) => p.box)), // 10. Everything together
];

/** The camera that fits `box` and a margin into the map area, centred on it and never zoomed in past 1.1. */
export function frame(box: Box): Camera {
	const s = Math.min(VIEW.w / (box.w + 2 * MARGIN), VIEW.mapH / (box.h + 2 * MARGIN), MAX_ZOOM);
	return { x: box.x + box.w / 2, y: box.y + box.h / 2, s };
}

/** 0 while a label would be under 7 px on screen, 1 from 8.5 px, and linear between. */
export function labelOpacity(s: number): number {
	const t = (LABEL_PX * s - LABEL_HIDDEN_PX) / (LABEL_SHOWN_PX - LABEL_HIDDEN_PX);
	return Math.min(1, Math.max(0, t));
}
