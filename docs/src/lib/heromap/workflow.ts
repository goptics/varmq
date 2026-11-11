// One workflow run on a worker's pool, for the chapter scripts. The workflow's rules are varmq's own state machine
// (`wfmachine.ts`, a port of wfmachine.go): what Submit pushes, what a finished task releases, what a failure
// cancels, and which undo jobs run. The pool simulation (`pool.ts`) says when each job runs. A workflow runs no job
// itself: a task that is ready becomes a job in its queue, and the job's outcome comes back to the workflow.

import { Machine, type TaskPlan, type WorkflowState } from '../../components/guides-a/wfmachine.ts';
import { simulatePool, type JobSpec, type PoolEvent, type PoolPlan } from './pool.ts';

/** What a task's chip shows (render.ts, `MARKS`): `queued` is the bare chip, from its push until its job ends. */
export type Mark = 'waiting' | 'queued' | 'done' | 'failed' | 'cancelled' | 'undone';

export interface TaskSpec extends TaskPlan {
	work: number;
	fails?: boolean;
	undoWork?: number; // how long its undo job runs: the task has an undo
}

export interface WorkflowPlan {
	id: string; // its jobs are `<id>-<task>`, and a task's undo job `<id>-<task>-undo`
	submit: number; // when Submit() is called
	promote: number; // ms from a push to the job's land in the queue
	undoAfter: number; // ms from the push of an undo job to its land
	tasks: TaskSpec[]; // in Add order
}

export interface WorkflowRun {
	submit: number;
	/** Each job's task, and whether it is the task's undo job. */
	jobs: Record<string, { task: string; undo: boolean }>;
	/** What each task shows, from when. Every task waits at the start. */
	marks: { at: number; task: string; mark: Mark }[];
	/** The workflow's state, from when: `running` at Submit. */
	states: { at: number; state: WorkflowState }[];
}

// Between the lands of jobs pushed at one instant: they leave the workflow one at a time, and the second lands once
// the first is taken. Two that wait together at limit 1 would leave as one batch, which the map cannot show (pool.ts).
const STAGGER = 500;

/**
 * Runs the workflow on a pool whose plan has no other jobs. Returns the jobs it adds, the pool's events, what each
 * job hands back, and the run for the choreographer.
 */
export function runWorkflow(wf: WorkflowPlan, pool: Omit<PoolPlan, 'jobs'>, source: string) {
	const specs = new Map(wf.tasks.map((t) => [t.name, t]));
	const machine = new Machine(wf.tasks.map(({ name, deps, always, undoWork }) => ({ name, deps, always, undo: undoWork !== undefined })));
	const jobs: JobSpec[] = [];
	const run: WorkflowRun = { submit: wf.submit, jobs: {}, marks: [], states: [{ at: wf.submit, state: 'running' }] };
	const outcomes: Record<string, 'value' | 'error'> = {};
	const shown = new Map<string, Mark>();
	const mark = (at: number, task: string, m: Mark) => {
		shown.set(task, m);
		run.marks.push({ at, task, mark: m });
	};

	/** After the machine has moved at `at`: its pushes become jobs, and what it cancelled or ended shows. */
	const apply = (fx: { push?: string; undo?: boolean }[], at: number) => {
		let k = 0;
		for (const e of fx) {
			if (!e.push) continue;
			const spec = specs.get(e.push)!;
			const id = `${wf.id}-${e.push}${e.undo ? '-undo' : ''}`;
			const land = at + (e.undo ? wf.undoAfter : wf.promote) + STAGGER * k++;
			jobs.push({ id, source, at: land, work: e.undo ? spec.undoWork! : spec.work });
			run.jobs[id] = { task: e.push, undo: !!e.undo };
			outcomes[id] = !e.undo && spec.fails ? 'error' : 'value';
			if (!e.undo) mark(at, e.push, 'queued');
		}
		for (const [name, t] of machine.tasks) {
			if (t.state === 'cancelled' && shown.get(name) !== 'cancelled') mark(at, name, 'cancelled');
		}
		if (machine.state !== run.states.at(-1)!.state) run.states.push({ at, state: machine.state });
	};

	apply(machine.roots, wf.submit);
	// A job added later changes nothing before its land, so each pass settles the next job that ends.
	const settled = new Set<string>();
	let events: PoolEvent[] = [];
	for (let guard = 0; ; guard++) {
		if (guard > 100) throw new Error(`workflow ${wf.id} does not settle`);
		events = simulatePool({ ...pool, jobs });
		const done = events.find((e) => e.type === 'done' && !settled.has(e.job));
		if (done?.type !== 'done') break;
		settled.add(done.job);
		const { task, undo } = run.jobs[done.job];
		const failed = !undo && !!specs.get(task)!.fails;
		const fx = machine.finish(task, failed ? 'failed' : 'completed', undo);
		mark(done.at, task, undo ? 'undone' : failed ? 'failed' : 'done');
		apply(fx, done.at);
	}
	return { jobs, events, outcomes, run };
}
