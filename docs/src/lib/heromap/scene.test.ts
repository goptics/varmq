import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { CONNS, LABEL_PX, PIECES, REGIONS, VIEW, frame, labelOpacity, slotX, slotY } from './scene.ts';

const inside = (a, b) => a.x >= b.x && a.y >= b.y && a.x + a.w <= b.x + b.w && a.y + a.h <= b.y + b.h;
const overlap = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
const view = c => ({ x: c.x - VIEW.w / 2 / c.s, y: c.y - VIEW.mapH / 2 / c.s, w: VIEW.w / c.s, h: VIEW.mapH / c.s });
// The narrowest the square is shown, in CSS px: HeroMap.astro's 23rem floor, which a short screen reaches. From
// 1090 px the square takes half the hero, up to 524 px, as long as the hero still fits the screen.
const MIN_SQUARE = 368;

describe('scene', () => {
	it('pads every lane 10 on both sides', () => {
		assert.equal(slotX(220, 0) + 11, 220 + 170 - 10);
		assert.equal(slotX(220, 4) - 11, 220 + 10);
		assert.equal(slotX(220, 1), slotX(220, 0) - 32);
		assert.equal(slotY(252), 274);
	});
	it('keeps children inside their parent and siblings apart', () => {
		const byId = Object.fromEntries(PIECES.map(p => [p.id, p]));
		for (const p of PIECES) if (p.parent) assert.ok(inside(p.box, byId[p.parent].box), p.id);
		for (const a of PIECES) for (const b of PIECES)
			if (a !== b && a.parent === b.parent && a.id < b.id) assert.ok(!overlap(a.box, b.box), `${a.id} × ${b.id}`);
	});
	it('frames chapters 1–9 with readable labels and the finale with everything', () => {
		for (let n = 1; n <= 9; n++) {
			const c = frame(REGIONS[n - 1]);
			assert.ok(inside(REGIONS[n - 1], view(c)), `chapter ${n}`);
			const px = (LABEL_PX * c.s * MIN_SQUARE) / VIEW.w;
			assert.equal(labelOpacity((c.s * MIN_SQUARE) / VIEW.w), 1, `chapter ${n} labels at ${px}px`);
		}
		assert.ok(frame(REGIONS[8]).s >= 0.6, `chapter 9 scale ${frame(REGIONS[8]).s}`);
		for (const p of PIECES) assert.ok(inside(p.box, view(frame(REGIONS[9]))), p.id);
	});
	it('fades labels between 6.5 and 7 px on screen', () => {
		assert.equal(labelOpacity(6.4 / LABEL_PX), 0);
		assert.equal(labelOpacity(7 / LABEL_PX), 1);
	});
	it('has unique ids and a chapter for every piece and line', () => {
		const ids = [...PIECES, ...CONNS].map(p => p.id);
		assert.equal(new Set(ids).size, ids.length);
		for (const p of [...PIECES, ...CONNS]) assert.ok(p.chapter >= 1 && p.chapter <= 9, p.id);
	});
});
