import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { CHAPTERS, TOTAL } from './chapters.ts';
import { buildTimeline } from './choreo.ts';
import { CONNS, PIECES, RINGS } from './scene.ts';

// The page without a DOM. Each element is a plain object that hands back the same child for the same selector, as
// the drawn map does, and the nodes refuse an id render.ts does not draw.
function element() {
	const children = new Map();
	const slots = Array.from({ length: 5 }, () => ({}));
	return {
		querySelector: sel => {
			if (!children.has(sel)) children.set(sel, element());
			return children.get(sel);
		},
		querySelectorAll: sel => (sel === '.vm-slot' ? slots : []),
	};
}

function lookup(ids, what, make) {
	const made = new Map();
	return id => {
		if (!ids.has(id)) throw new Error(`no ${what} ${id}`);
		if (!made.has(id)) made.set(id, make(id));
		return made.get(id);
	};
}

const chips = CHAPTERS.flatMap(c => (c.chips ?? []).map(chip => `${chip.id}@${c.n}`));
const stores = CHAPTERS.flatMap(c => c.parts.filter(p => p.kind !== 'pool'));
const jobs = CHAPTERS.flatMap(c => [...(c.published ?? []).map(v => v.id), ...c.parts.flatMap(p => p.plan.jobs.map(j => j.id))]);
const rings = Object.entries(RINGS).flatMap(([worker, places]) => places.map((_, i) => `${worker}:${i}`));
const captions = CHAPTERS.flatMap(c => c.captions.map((cap, i) => [`${c.n}:${i}`, cap.text.split(' ').length]));
const words = new Map(captions);

const ring = lookup(new Set(rings), 'ring', () => ({ ring: element(), progress: element(), check: element(), ghost: element() }));
const line = lookup(new Set(words.keys()), 'caption', key => Array.from({ length: words.get(key) }, element));
const nodes = {
	svg: element(),
	world: element(),
	caption: element(),
	piece: lookup(new Set([...PIECES.map(p => p.id), ...CONNS.map(c => c.id), ...chips]), 'piece', element),
	ring: (worker, i) => ring(`${worker}:${i}`),
	job: lookup(new Set(jobs), 'job', () => [element(), element(), element()]),
	record: lookup(new Set(stores.flatMap(p => p.plan.jobs.map(j => j.id))), 'record', element),
	words: (n, i) => line(`${n}:${i}`),
	setCamera: () => {},
	setWidth: () => {},
};
const page = {
	path: pts => ({ translateX: [pts[0].x, pts.at(-1).x], translateY: [pts[0].y, pts.at(-1).y] }),
	drawable: el => el,
	dot: () => element(),
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
});
