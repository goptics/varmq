import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { arcPath, HALF, QUARTER, segment, stepAt, tickAngle, tickPoint } from './arc.ts';

const near = (a: number, b: number) => assert.ok(Math.abs(a - b) < 1e-9, `${a} ≈ ${b}`);

describe('the tour step arc', () => {
	it('cuts the span in a part per step, step 1 at its start, each step in the middle of its part', () => {
		near(tickAngle(0, 4, HALF), 157.5);
		near(tickAngle(5, 11, HALF), 90);
		near(tickAngle(3, 4, HALF), 22.5);
		near(tickAngle(1, 2, QUARTER), 112.5);
		const s = segment(0, 4, HALF);
		near(s.from, 174.375);
		near(s.to, 140.625);
		near(segment(3, 4, HALF).to, 5.625);
		const p = tickPoint(5, 11, HALF, 40);
		near(p.x, 0);
		near(p.y, -40);
	});

	it('picks the nearest step by angle, clamped at both ends', () => {
		assert.equal(stepAt(-50, 0, 11, HALF), 0);
		assert.equal(stepAt(0, -50, 11, HALF), 5);
		assert.equal(stepAt(50, 0, 11, HALF), 10);
		assert.equal(stepAt(-50, 10, 11, HALF), 0); // below, left
		assert.equal(stepAt(50, 10, 11, HALF), 10); // below, right
		assert.equal(stepAt(0, -50, 11, QUARTER), 10);
		assert.equal(stepAt(50, -50, 11, QUARTER), 10); // right of the quarter
		assert.equal(stepAt(-50, 10, 11, QUARTER), 0);
		// Each step owns its whole part of the span: with 4 steps over HALF, step 1 to 135°, step 2 from there.
		const at = (deg: number) => stepAt(Math.cos((deg * Math.PI) / 180), -Math.sin((deg * Math.PI) / 180), 4, HALF);
		assert.equal(at(136), 0);
		assert.equal(at(134), 1);
		assert.equal(at(1), 3);
	});

	it('draws the arc left to right over the top', () => {
		assert.equal(arcPath(HALF, 40), 'M -40 0 A 40 40 0 0 1 40 0');
		assert.equal(arcPath(QUARTER, 40), 'M -40 0 A 40 40 0 0 1 0 -40');
	});
});
