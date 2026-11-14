import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { arcPath, HALF, QUARTER, stepAt, tickAngle, tickPoint } from './arc.ts';

const near = (a: number, b: number) => assert.ok(Math.abs(a - b) < 1e-9, `${a} ≈ ${b}`);

describe('the tour step arc', () => {
	it('spreads ticks over the span, step 1 at its start', () => {
		near(tickAngle(0, 11, HALF), 180);
		near(tickAngle(5, 11, HALF), 90);
		near(tickAngle(10, 11, HALF), 0);
		near(tickAngle(10, 11, QUARTER), 90);
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
	});

	it('draws the arc left to right over the top', () => {
		assert.equal(arcPath(HALF, 40), 'M -40 0 A 40 40 0 0 1 40 0');
		assert.equal(arcPath(QUARTER, 40), 'M -40 0 A 40 40 0 0 1 0 -40');
	});
});
