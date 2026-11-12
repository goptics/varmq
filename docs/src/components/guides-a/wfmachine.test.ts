import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { Machine, type Effect, type Outcome, type TaskPlan, type WorkflowState } from './wfmachine.ts';

// Cases from TestMachine in workflow_test.go: each step is a finish (or a cancel) and the effects it must give.
type Step = [task: string, out: Outcome | 'cancel', effects: string[], undo?: boolean];

const fx = (e: Effect[]) => e.flatMap((x) => [...(x.push ? [x.push + (x.undo ? ':undo' : '')] : []), ...(x.end ? [`end:${x.end}`] : [])]);

function replay(plan: TaskPlan[], failFast: boolean, roots: string[], steps: Step[], final: WorkflowState): Machine {
	const m = new Machine(plan, failFast);
	assert.deepEqual(fx(m.roots), roots);
	for (const [task, out, want, undo] of steps) {
		const got = out === 'cancel' ? m.cancel(undo) : m.finish(task, out, undo);
		assert.deepEqual(fx(got), want, `${task} ${out}`);
	}
	assert.equal(m.state, final);
	return m;
}

const ok = 'completed';
const bad = 'failed';
const states = (m: Machine) => Object.fromEntries(m.order.map((n) => [n, m.tasks.get(n)!.state]));

describe('workflow machine (TestMachine)', () => {
	it('chain completed', () => {
		replay([{ name: 'A' }, { name: 'B', deps: ['A'] }, { name: 'C', deps: ['B'] }], false, ['A'],
			[['A', ok, ['B']], ['B', ok, ['C']], ['C', ok, ['end:completed']]], 'completed');
	});
	it('diamond, B failed, D Always: absorbed', () => {
		const m = replay([{ name: 'A' }, { name: 'B', deps: ['A'] }, { name: 'C', deps: ['A'] }, { name: 'D', deps: ['B', 'C'], always: true }], false, ['A'],
			[['A', ok, ['B', 'C']], ['B', bad, []], ['C', ok, ['D']], ['D', ok, ['end:completed']]], 'completed');
		assert.deepEqual(states(m), { A: 'completed', B: 'failed', C: 'completed', D: 'completed' });
	});
	it('diamond, B failed, D not Always: cancelled, failed', () => {
		const m = replay([{ name: 'A' }, { name: 'B', deps: ['A'] }, { name: 'C', deps: ['A'] }, { name: 'D', deps: ['B', 'C'] }], false, ['A'],
			[['A', ok, ['B', 'C']], ['B', bad, []], ['C', ok, ['end:failed']]], 'failed');
		assert.equal(m.tasks.get('D')!.state, 'cancelled');
	});
	it('cancel recursion: C Always after cancelled B', () => {
		const m = replay([{ name: 'A' }, { name: 'B', deps: ['A'] }, { name: 'C', deps: ['B'], always: true }], false, ['A'],
			[['A', bad, ['C']], ['C', ok, ['end:failed']]], 'failed');
		assert.deepEqual(states(m), { A: 'failed', B: 'cancelled', C: 'completed' });
	});
	it('undo in reverse order, compensated', () => {
		replay([{ name: 'A', undo: true }, { name: 'B', deps: ['A'], undo: true }, { name: 'C', deps: ['B'] }], false, ['A'],
			[['A', ok, ['B']], ['B', ok, ['C']], ['C', bad, ['B:undo']], ['B', ok, ['A:undo'], true], ['A', ok, ['end:compensated'], true]], 'compensated');
	});
	it('undo fails, others still run: compensation_failed', () => {
		replay([{ name: 'A', undo: true }, { name: 'B', deps: ['A'], undo: true }, { name: 'C', deps: ['B'] }], false, ['A'],
			[['A', ok, ['B']], ['B', ok, ['C']], ['C', bad, ['B:undo']], ['B', bad, ['A:undo'], true], ['A', ok, ['end:compensation_failed'], true]], 'compensation_failed');
	});
	it('saga order looks through a task without undo', () => {
		replay([{ name: 'A', undo: true }, { name: 'B', deps: ['A'] }, { name: 'C', deps: ['B'], undo: true }, { name: 'D', deps: ['C'] }], false, ['A'],
			[['A', ok, ['B']], ['B', ok, ['C']], ['C', ok, ['D']], ['D', bad, ['C:undo']], ['C', ok, ['A:undo'], true], ['A', ok, ['end:compensated'], true]], 'compensated');
	});
	it('fan-in undo waits for every completed dependent undo', () => {
		replay([{ name: 'A', undo: true }, { name: 'B', deps: ['A'], undo: true }, { name: 'C', deps: ['A'], undo: true }, { name: 'D', deps: ['B', 'C'] }], false, ['A'],
			[['A', ok, ['B', 'C']], ['B', ok, []], ['C', ok, ['D']], ['D', bad, ['B:undo', 'C:undo']], ['C', ok, [], true], ['B', ok, ['A:undo'], true], ['A', ok, ['end:compensated'], true]], 'compensated');
	});
	it('FailFast cancels waiting at once', () => {
		const m = replay([{ name: 'A' }, { name: 'B', deps: ['A'] }, { name: 'X' }, { name: 'Y', deps: ['X'] }], true, ['A', 'X'],
			[['A', bad, []], ['X', ok, ['end:failed']]], 'failed');
		assert.deepEqual(states(m), { A: 'failed', B: 'cancelled', X: 'completed', Y: 'cancelled' });
	});
	it('no FailFast lets unrelated branches finish', () => {
		replay([{ name: 'A' }, { name: 'B', deps: ['A'] }, { name: 'X' }, { name: 'Y', deps: ['X'] }], false, ['A', 'X'],
			[['A', bad, []], ['X', ok, ['Y']], ['Y', ok, ['end:failed']]], 'failed');
	});
	it('FailFast fires on the first unabsorbed failure only', () => {
		const m = replay([{ name: 'A' }, { name: 'P' }, { name: 'Q' }, { name: 'D', deps: ['A', 'P'], always: true }, { name: 'E', deps: ['A'] }], true, ['A', 'P', 'Q'],
			[['A', bad, []], ['Q', bad, []], ['P', ok, ['D']], ['D', ok, ['end:failed']]], 'failed');
		assert.equal(m.tasks.get('E')!.state, 'cancelled');
	});
	it('cancel with undo compensates completed tasks; queued tasks fail the pre-run check', () => {
		const m = new Machine([{ name: 'A', undo: true }, { name: 'B', deps: ['A'] }, { name: 'C', deps: ['B'] }]);
		m.finish('A', ok);
		assert.deepEqual(fx(m.cancel()), []);
		assert.equal(m.runnable('B'), false);
		assert.deepEqual(fx(m.finish('B', 'cancelled')), ['A:undo']);
		assert.equal(m.state, 'compensating');
	});
	it('the workflows-undo example ends compensated', () => {
		// reserve (undo) -> label -> charge, and charge fails (docs/examples/workflows-undo).
		replay([{ name: 'reserve', undo: true }, { name: 'label', deps: ['reserve'] }, { name: 'charge', deps: ['label'] }], false, ['reserve'],
			[['reserve', ok, ['label']], ['label', ok, ['charge']], ['charge', bad, ['reserve:undo']], ['reserve', ok, ['end:compensated'], true]], 'compensated');
	});
});
