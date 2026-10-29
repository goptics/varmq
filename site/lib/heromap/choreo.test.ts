import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { CHAPTERS, TOTAL, chipKey } from './chapters.ts';
import { Choreo, buildTimeline } from './choreo.ts';
import { CONNS, PIECES, RINGS } from './scene.ts';

// The page without a DOM. Each element is a plain object with a name and its parent, that hands back the same child
// for the same selector, as the drawn map does, and the nodes refuse an id render.ts does not draw.
function element(name, parent) {
	const children = new Map();
	let slots;
	const el = {
		name,
		parent,
		querySelector: sel => {
			if (!children.has(sel)) children.set(sel, element(`${name} ${sel}`, el));
			return children.get(sel);
		},
		querySelectorAll: sel =>
			sel === '.vm-slot' ? (slots ??= Array.from({ length: 5 }, (_, k) => element(`${name} slot ${k}`, el))) : [],
	};
	return el;
}

function lookup(ids, what, make) {
	const made = new Map();
	return id => {
		if (!ids.has(id)) throw new Error(`no ${what} ${id}`);
		if (!made.has(id)) made.set(id, make(id));
		return made.get(id);
	};
}

const chips = CHAPTERS.flatMap(c => (c.chips ?? []).map((chip, i) => chipKey(c.n, i, chip.id)));
const stores = CHAPTERS.flatMap(c => c.parts.filter(p => p.kind !== 'pool'));
const jobs = CHAPTERS.flatMap(c => [...(c.published ?? []).map(v => v.id), ...c.parts.flatMap(p => p.plan.jobs.map(j => j.id))]);
const rings = Object.entries(RINGS).flatMap(([worker, places]) => places.map((_, i) => `${worker}:${i}`));
const captions = CHAPTERS.flatMap(c => c.captions.map((cap, i) => [`${c.n}:${i}`, cap.text.split(' ').length]));
const words = new Map(captions);

const ring = lookup(new Set(rings), 'ring', id => ({
	ring: element(`${id} ring`),
	progress: element(`${id} progress`),
	check: element(`${id} check`),
	ghost: element(`${id} ghost`),
}));
const line = lookup(new Set(words.keys()), 'caption', key =>
	Array.from({ length: words.get(key) }, (_, i) => element(`caption ${key} word ${i}`)),
);
const swaps = new Set(CHAPTERS.flatMap(c => c.captions.flatMap((cap, i) => (cap.swap ? [`${c.n}:${i}`] : []))));
const swapWord = lookup(swaps, 'caption swap', key => element(`caption ${key} swap`));
const nodes = {
	svg: element('svg'),
	world: element('world'),
	caption: element('caption'),
	piece: lookup(new Set([...PIECES.map(p => p.id), ...CONNS.map(c => c.id), ...chips]), 'piece', element),
	ring: (worker, i) => ring(`${worker}:${i}`),
	job: lookup(new Set(jobs), 'job', id => [element(id), element(`${id} trail 1`), element(`${id} trail 2`)]),
	record: lookup(new Set(stores.flatMap(p => p.plan.jobs.map(j => j.id))), 'record', id => element(`record ${id}`)),
	words: (n, i) => line(`${n}:${i}`),
	swap: (n, i) => swapWord(`${n}:${i}`),
	setCamera: () => {},
	setWidth: () => {},
};
const page = {
	path: pts => ({ translateX: [pts[0].x, pts.at(-1).x], translateY: [pts[0].y, pts.at(-1).y] }),
	drawable: el => el,
	dot: () => element('dot'),
};

describe('choreography', () => {
	it('builds the whole loop, all ten chapters, with nothing refused', () => {
		// The choreographer refuses what it cannot draw: a lane with no slot free for a landing, a property changed out
		// of order, a tween past the loop's end, a set with no room for its pad. Any of them fails this test.
		const timeline = buildTimeline(nodes, page);
		assert.equal(timeline.iterationDuration, TOTAL);
		// And no set ends its property's chain (see score.ts).
		for (let child = timeline._head; child; child = child._next) {
			for (let tween = child._head; tween; tween = tween._next) {
				if (!tween._nextRep) assert.ok(tween._updateDuration >= 1, `${tween.property} at ${tween._absoluteStartTime} ms`);
			}
		}
	});

	it('ends the loop on its first frame, so it wraps without a seam', () => {
		// Every property at the loop's end against its start, wherever it shows at either. A target shows nothing at
		// opacity or scale 0, as an outline with nothing drawn and no fill, or inside a parent that shows nothing.
		const choreo = new Choreo(nodes, page);
		choreo.build();
		const tracks = new Map();
		for (const [target, track] of choreo.score.all()) tracks.set(target, [...(tracks.get(target) ?? []), track]);
		const value = (target, prop, t) => tracks.get(target)?.find(k => k.name === prop)?.at(t);
		const chain = target => (target ? [target, ...chain(target.parent)] : []);
		const shows = (target, t) => {
			if (chain(target).some(el => value(el, 'opacity', t)?.[0] === 0 || value(el, 'scale', t)?.[0] === 0)) return false;
			const draw = value(target, 'draw', t);
			const fill = value(target, 'fillOpacity', t);
			return (!draw && !fill) || draw?.[1] > draw?.[0] || fill?.[0] > 0;
		};
		const parents = new Set([...tracks.keys()].map(target => target.parent));
		const seams = new Set();
		for (const leaf of tracks.keys()) {
			if (parents.has(leaf) || !(shows(leaf, 0) || shows(leaf, TOTAL))) continue;
			for (const el of chain(leaf)) {
				for (const track of tracks.get(el) ?? []) {
					const [start, end] = [track.at(0), track.at(TOTAL)];
					if (start.some((v, i) => Math.abs(v - end[i]) > 1e-6)) {
						seams.add(`${el.name ?? 'camera'} ${track.name}: ${end} at the end, ${start} at the start`);
					}
				}
			}
		}
		assert.deepEqual([...seams], []);
	});
});
