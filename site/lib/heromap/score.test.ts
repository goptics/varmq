import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { PAD, SET_EARLY, Score } from './score.ts';

const TOTAL = 1000;
const ease = t => t;
// The page's stage without a page: a route by its ends, an outline as its own stand-in.
const stage = { path: pts => ({ translateX: [pts[0].x, pts.at(-1).x], translateY: [pts[0].y, pts.at(-1).y] }), drawable: el => el };

// One of each way a track can end, on plain objects.
function sample() {
	const s = new Score(TOTAL, stage);
	const t = {
		shown: { opacity: 0 }, // tweened in, then set out: ends in a set
		hidden: { opacity: 1 }, // set out, then tweened in: ends in a tween
		still: { opacity: 0.5 }, // only its start, a set at 0
		square: { translateX: 0, translateY: 0 }, // moved, then put back
		words: [{ opacity: 0 }, { opacity: 0 }], // staggered in, the first set out
		outline: { draw: '0 0' }, // drawn, then set blank
	};
	s.start(t.shown, { opacity: 0 });
	s.tween(t.shown, { opacity: 1 }, 100, 200, ease);
	s.set(t.shown, { opacity: 0 }, 500);
	s.start(t.hidden, { opacity: 1 });
	s.set(t.hidden, { opacity: 0 }, 100);
	s.tween(t.hidden, { opacity: 1 }, 300, 200, ease);
	s.start(t.still, { opacity: 0.5 });
	s.start(t.square, { pos: [0, 0] });
	s.move(t.square, [{ x: 10, y: 0 }, { x: 10, y: 10 }], 100, 300, ease);
	s.place(t.square, { x: 0, y: 0 }, 600);
	for (const w of t.words) s.start(w, { opacity: 0 });
	s.stagger(t.words, { opacity: 1 }, 100, 50, 200, ease);
	s.set(t.words[0], { opacity: 0 }, 700);
	const draw = s.drawable(t.outline);
	s.start(draw, { draw: [0, 0] });
	s.tween(draw, { draw: [0, 1] }, 100, 200, ease);
	s.set(draw, { draw: [0, 0] }, 800);
	return { s, t, timeline: s.build(() => {}) };
}

describe('score', () => {
	const { s, t, timeline } = sample();
	const last = (target, prop) => s.track(target, prop).segs.at(-1);

	it('ends no track in a set', () => {
		for (const [, track] of s.all()) {
			assert.ok(!track.endsInSet, track.name);
			assert.ok(track.end <= TOTAL, track.name);
		}
	});

	it('pads a set that ends its track right after it, holding its value', () => {
		const pad = last(t.shown, 'opacity');
		assert.equal(pad.start, 500 - SET_EARLY + PAD.after);
		assert.equal(pad.end - pad.start, PAD.duration);
		assert.deepEqual(pad.at(pad.start), [0]);
		assert.equal(last(t.still, 'opacity').start, PAD.after);
		assert.deepEqual(last(t.square, 'pos').at(TOTAL), [0, 0]);
		assert.deepEqual(last(t.outline, 'draw').at(TOTAL), [0, 0]);
	});

	it('pads no track that ends in a tween', () => {
		assert.equal(last(t.hidden, 'opacity').start, 300);
		assert.equal(last(t.words[1], 'opacity').start, 150);
		assert.equal(last(t.words[0], 'opacity').start, 700 - SET_EARLY + PAD.after);
	});

	it('leaves no set last among the tweens on its property in the timeline', () => {
		let finals = 0;
		for (let child = timeline._head; child; child = child._next) {
			for (let tween = child._head; tween; tween = tween._next) {
				if (tween._nextRep) continue;
				finals++;
				const where = `${tween.property} at ${tween._absoluteStartTime} ms`;
				assert.ok(tween._updateDuration >= PAD.duration, `${where} is a set`);
				assert.ok(tween._absoluteEndTime <= TOTAL, `${where} runs past the loop`);
				if (tween._prevRep) assert.ok(tween._absoluteStartTime >= tween._prevRep._absoluteEndTime, `${where} overlaps`);
			}
		}
		// opacity of shown, hidden, still and both words; translateX/Y; draw.
		assert.equal(finals, 8);
	});
});
