import { test } from 'node:test';
import assert from 'node:assert/strict';
import { WorkerSim } from './sim.ts';

const HOLD = 600_000;

test('a limit of 100 with three jobs runs three, not one hundred', () => {
	const w = new WorkerSim({ limit: 100 });
	assert.equal(w.numIdleWorkers(), 1);
	for (let i = 0; i < 3; i++) {
		w.add({ dur: HOLD });
		w.advance(40);
	}
	assert.equal(w.numProcessing(), 3);
	assert.equal(w.numIdleWorkers(), 0);
	assert.equal(w.numPending(), 0);
	assert.equal(w.numConcurrency(), 100);
});

test('TunePool raises the limit for jobs that wait, and a repeat is ErrSameConcurrency', () => {
	const w = new WorkerSim({ limit: 1 });
	w.add({ dur: HOLD });
	w.advance(40);
	assert.equal(w.tunePool(3), null);
	for (let i = 0; i < 5; i++) w.add({ dur: HOLD });
	w.advance(40);
	assert.equal(w.numProcessing(), 3);
	assert.equal(w.numPending(), 3);
	assert.equal(w.tunePool(3), 'ErrSameConcurrency');
});

test('a lone goroutine takes the backlog in one batch, and a raise does not spread it', () => {
	const w = new WorkerSim({ limit: 1 });
	w.add({ dur: 1000 });
	w.advance(40);
	for (let i = 0; i < 4; i++) w.add({ dur: 1000 });
	assert.equal(w.queueLen(), 4);
	w.advance(1000);
	// Job 2 runs, jobs 3 to 5 are in its batch: pending but not in the queue.
	assert.equal(w.queueLen(), 0);
	assert.equal(w.numPending(), 3);
	w.tunePool(4);
	w.advance(40);
	assert.equal(w.numProcessing(), 1);
});

test('a lowered limit stops new hand-offs until fewer goroutines are busy', () => {
	const w = new WorkerSim({ limit: 6 });
	for (let i = 0; i < 9; i++) w.add({ dur: 1000 });
	w.advance(40);
	assert.equal(w.numProcessing(), 6);
	w.tunePool(2);
	assert.equal(w.numProcessing(), 6);
	w.advance(1000);
	// Six jobs end; hand-offs start only while busy is below 2.
	assert.equal(w.numProcessing(), 2);
	assert.equal(w.numPending(), 1);
});

test('the metrics equation holds on an idle worker, a paused worker, and breaks after Purge', () => {
	const w = new WorkerSim({ limit: 1 });
	w.add({ dur: 100 });
	w.advance(400);
	const sum = () => w.numPending() + w.numProcessing() + w.m.completed;
	assert.equal(w.status, 'Idle');
	assert.equal(w.m.submitted, sum());
	w.pause();
	w.add();
	w.add();
	assert.equal(w.status, 'Paused');
	assert.equal(w.m.submitted, 3);
	assert.equal(sum(), 3);
	w.purge();
	assert.equal(sum(), 1);
	assert.equal(w.m.submitted, 3);
});

test('a cancelled context lets running jobs finish, keeps waiting jobs queued and reports no error', () => {
	const w = new WorkerSim({ limit: 1 });
	w.add({ dur: 1000 });
	w.advance(40);
	w.add();
	w.add();
	w.cancelUser('canceled');
	w.advance(40);
	assert.equal(w.status, 'Stopping');
	assert.equal(w.numProcessing(), 1);
	w.advance(1000);
	assert.equal(w.status, 'Stopped');
	assert.equal(w.numPending(), 2);
	assert.equal(w.queueLen(), 2);
	assert.equal(w.m.failed, 0);
	w.add();
	assert.equal(w.queueLen(), 3);
	assert.equal(w.ctx, 'canceled');
});

test('a cancelled context cannot be started again', () => {
	const w = new WorkerSim({ limit: 1 });
	w.cancelUser('deadline');
	w.advance(40);
	assert.equal(w.status, 'Stopped');
	assert.equal(w.start(), null);
	w.advance(40);
	assert.equal(w.status, 'Stopped');
	assert.equal(w.ctx, 'deadline');
});

test('Stop then Restart brings the worker back and its parked jobs run first', () => {
	const w = new WorkerSim({ limit: 1 });
	w.add({ dur: 1000 });
	w.advance(40);
	for (let i = 0; i < 3; i++) w.add({ dur: 1000 });
	w.advance(1000);
	// Job 2 runs; jobs 3 and 4 wait in its batch.
	w.stop();
	w.advance(1000);
	assert.equal(w.status, 'Stopped');
	assert.equal(w.parked.length, 2);
	assert.equal(w.numPending(), 2);
	assert.equal(w.restart(), null);
	w.advance(40);
	assert.equal(w.status, 'Running');
	assert.equal(w.ctx, null);
});

test('Wait returns on a pause while jobs are pending; WaitUntilIdle keeps waiting', () => {
	const w = new WorkerSim({ limit: 1 });
	w.add({ dur: 1000 });
	w.advance(40);
	w.add();
	w.add();
	w.pause();
	assert.equal(w.status, 'Pausing');
	assert.equal(w.waitDone(), false);
	w.advance(1000);
	assert.equal(w.status, 'Paused');
	assert.equal(w.waitDone(), true);
	assert.equal(w.numPending(), 2);
	assert.equal(w.waitUntilIdleDone(), false);
	w.resume();
	w.advance(6000);
	assert.equal(w.status, 'Idle');
	assert.equal(w.waitUntilIdleDone(), true);
});

test('status errors match getStatusError', () => {
	const w = new WorkerSim({ limit: 1, autoRun: false });
	assert.equal(w.status, 'Initiated');
	assert.equal(w.tunePool(2), 'ErrNotRunningWorker');
	assert.equal(w.start(), null);
	assert.equal(w.start(), 'ErrRunningWorker');
	w.pause();
	assert.equal(w.tunePool(2), 'ErrWorkerPaused');
	assert.equal(w.start(), 'ErrWorkerPaused');
	w.stop();
	w.advance(40);
	assert.equal(w.resume(), 'ErrWorkerStopped');
});

test('idle goroutines: the default keeps one, a ratio keeps limit * percent / 100', () => {
	const a = new WorkerSim({ limit: 4 });
	assert.equal(a.minIdle(), 1);
	a.minIdlePct = 50;
	assert.equal(a.minIdle(), 2);
	a.minIdlePct = 100;
	assert.equal(a.minIdle(), 4);
	const b = new WorkerSim({ limit: 200 });
	b.minIdlePct = 1;
	assert.equal(b.minIdle(), 2);
});

test('an expiry timer removes goroutines idle longer than the duration, down to the minimum', () => {
	const w = new WorkerSim({ limit: 4 });
	w.expiry = 1000;
	for (let i = 0; i < 4; i++) w.add({ dur: 500 });
	w.advance(40);
	assert.equal(w.numProcessing(), 4);
	w.advance(600);
	assert.equal(w.numIdleWorkers(), 4);
	w.advance(3000);
	assert.equal(w.numIdleWorkers(), 1);
});
