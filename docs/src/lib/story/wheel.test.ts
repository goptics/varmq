import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { wheelSteps } from './wheel.ts';

/** The times at which a stream of wheel events, each [time, delta], moves a step. */
const steps = (events: [number, number][]) => {
	const turn = wheelSteps();
	return events.filter(([time, delta]) => turn(time, delta)).map(([time]) => time);
};

describe('the story wheel', () => {
	it('moves one step per notch of a wheel turned notch by notch, and one for a quick flick of a few', () => {
		assert.deepEqual(
			steps([
				[0, 120],
				[400, 120],
				[800, 120],
			]),
			[0, 400, 800],
		);
		assert.deepEqual(
			steps([
				[0, 120],
				[25, 120],
				[50, 120],
				[75, 120],
			]),
			[0],
		);
	});

	it('keeps stepping while a free-spinning wheel keeps its speed, though Chrome holds notches back after each step', () => {
		// An MX Master spinning freely, as recorded on Linux: a 120 px notch about every 8 ms. Each step starts work that
		// holds the page for about 90 ms, after which Chrome sends the notches it held back as one event.
		const turn = wheelSteps();
		const at: number[] = [];
		let held = 0;
		let until = -1;
		for (let time = 0; time <= 4000; time += 8) {
			if (time < until) {
				held += 120;
				continue;
			}
			const delta = 120 + held;
			held = 0;
			if (turn(time, delta)) {
				at.push(time);
				until = time + 90;
			}
		}
		assert.ok(at.length >= 6, `only ${at.length} steps in a 4 s spin`);
		for (let i = 1; i < at.length; i++) assert.ok(at[i] - at[i - 1] < 800, `no step for ${at[i] - at[i - 1]} ms`);
	});

	it('moves one step for a swipe, not for its momentum', () => {
		// A trackpad swipe: a delta every 16 ms that builds up, then dies away by 8% a frame.
		const deltas = [4, 10, 24, 40, 48, 44];
		while (deltas.at(-1)! > 0.5) deltas.push(deltas.at(-1)! * 0.92);
		assert.equal(steps(deltas.map((delta, i) => [i * 16, delta])).length, 1);
	});
});
