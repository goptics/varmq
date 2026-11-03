import assert from 'node:assert/strict';
import { it } from 'node:test';
import { STEPS, STOPS } from './steps.ts';

it('has one scroll stop per caption, in order', () => {
	assert.equal(STOPS.length, STEPS.reduce((n, s) => n + s.captions.length, 0));
	STOPS.forEach((stop, n) => {
		const prev = STOPS[n - 1] ?? { step: 0, caption: -1 };
		const next =
			stop.step === prev.step ? { step: prev.step, caption: prev.caption + 1 } : { step: prev.step + 1, caption: 0 };
		assert.deepEqual(stop, next);
	});
});
