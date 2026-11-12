// wfmachine.go in TypeScript: one workflow's state machine, so the workflow scene never shows a result varmq cannot
// produce. Same names, same walks in Add order. wfmachine.test.ts replays Go's TestMachine cases.

export type JobState = '' | 'waiting' | 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled';
export type WorkflowState = 'running' | 'completed' | 'failed' | 'compensating' | 'compensated' | 'compensation_failed' | 'cancelled';
export type Outcome = 'completed' | 'failed' | 'cancelled';

export interface TaskPlan {
	name: string;
	deps?: string[];
	always?: boolean;
	undo?: boolean;
}

export interface Task {
	plan: TaskPlan;
	state: JobState;
	open: number;
	undoState: JobState;
	undoOpen: number;
}

/** One thing storage must do: push a task (or its undo), and/or end the workflow. */
export interface Effect {
	push?: string;
	undo?: boolean;
	end?: WorkflowState;
}

const active = (s: JobState) => s === 'pending' || s === 'processing';

export class Machine {
	state: WorkflowState = 'running';
	tasks = new Map<string, Task>();
	order: string[] = [];
	private next = new Map<string, string[]>();
	private failed = false;
	private cancelled = false;

	/** The start state; `roots` are the pushes Submit does. */
	readonly roots: Effect[] = [];

	failFast: boolean;
	undoOnCancel: boolean;

	constructor(plan: TaskPlan[], failFast = false, undoOnCancel = true) {
		this.failFast = failFast;
		this.undoOnCancel = undoOnCancel;
		for (const tp of plan) {
			const open = tp.deps?.length ?? 0;
			this.tasks.set(tp.name, { plan: tp, state: open ? 'waiting' : 'pending', open, undoState: '', undoOpen: 0 });
			if (!open) this.roots.push({ push: tp.name });
			this.order.push(tp.name);
			for (const d of tp.deps ?? []) this.next.set(d, [...(this.next.get(d) ?? []), tp.name]);
		}
	}

	private t(name: string): Task {
		const t = this.tasks.get(name);
		if (!t) throw new Error(`no task ${name}`);
		return t;
	}

	/** A task's (or undo's) outcome. A late or repeated finish does nothing. */
	finish(task: string, out: Outcome, undo = false): Effect[] {
		const t = this.t(task);
		if (undo) return this.finishUndo(task, t, out);
		if (this.state !== 'running' || !active(t.state)) return [];
		const push = new Set<string>();
		if (out === 'completed') {
			t.state = 'completed';
			for (const d of this.next.get(task) ?? []) this.release(d, push);
			return this.settle(push);
		}
		t.state = out;
		// After cancel, ends (pre-run cancels included) are not failures.
		if (!this.cancelled && !this.absorbed(task)) {
			if (this.failFast && !this.failed) this.doFailFast(task, push);
			this.failed = true;
		}
		this.drop(task, push);
		return this.settle(push);
	}

	/** Cancels waiting tasks now; queued ones end at the pre-run check. */
	cancel(undo = this.undoOnCancel): Effect[] {
		if (this.state !== 'running' || this.cancelled) return [];
		this.cancelled = true;
		this.undoOnCancel = undo;
		for (const name of this.order) {
			const t = this.t(name);
			if (t.state === 'waiting') t.state = 'cancelled';
		}
		return this.settle(new Set());
	}

	/** The pre-run check: may this task (or its undo) start now. */
	runnable(task: string, undo = false): boolean {
		const t = this.t(task);
		if (undo) return this.state === 'compensating' && t.undoState === 'pending';
		const stopped = this.failFast && this.failed && !t.plan.always;
		return this.state === 'running' && !this.cancelled && !stopped && t.state === 'pending';
	}

	private finishUndo(task: string, t: Task, out: Outcome): Effect[] {
		if (this.state !== 'compensating' || !active(t.undoState)) return [];
		t.undoState = out === 'completed' ? 'completed' : 'failed';
		const push = new Set<string>();
		const up = this.reach(task, true);
		for (const name of this.order) {
			const x = this.t(name);
			if (!up.has(name) || x.undoState !== 'waiting') continue;
			if (--x.undoOpen === 0) {
				x.undoState = 'pending';
				push.add(name);
			}
		}
		const fx = this.emit(push, true);
		let failed = false;
		for (const name of this.order) {
			const s = this.t(name).undoState;
			if (s === 'waiting' || active(s)) return fx;
			if (s === 'failed') failed = true;
		}
		return this.end(fx, failed ? 'compensation_failed' : 'compensated');
	}

	private release(name: string, push: Set<string>) {
		const t = this.t(name);
		if (t.state !== 'waiting') return;
		if (--t.open === 0) {
			t.state = 'pending';
			push.add(name);
		}
	}

	/** A failed or cancelled task's dependents: Always ones count it as ended, others are cancelled, recursively. */
	private drop(name: string, push: Set<string>) {
		for (const d of this.next.get(name) ?? []) {
			const t = this.t(d);
			if (t.state !== 'waiting') continue;
			if (t.plan.always) {
				this.release(d, push);
				continue;
			}
			t.state = 'cancelled';
			this.drop(d, push);
		}
	}

	/** The task has dependents and every one is Always. */
	private absorbed(name: string): boolean {
		const next = this.next.get(name) ?? [];
		return next.length > 0 && next.every((d) => this.t(d).plan.always);
	}

	private doFailFast(failed: string, push: Set<string>) {
		const keep = new Set((this.next.get(failed) ?? []).filter((d) => this.t(d).plan.always));
		const cancelled = this.order.filter((name) => {
			const t = this.t(name);
			if (t.state !== 'waiting' || keep.has(name)) return false;
			t.state = 'cancelled';
			return true;
		});
		for (const name of cancelled) this.drop(name, push);
	}

	/** Every task reachable from name: ancestors when up, else descendants. */
	private reach(name: string, up: boolean): Set<string> {
		const seen = new Set<string>();
		const stack = [name];
		while (stack.length) {
			const n = stack.pop()!;
			for (const m of up ? (this.t(n).plan.deps ?? []) : (this.next.get(n) ?? [])) {
				if (!seen.has(m)) {
					seen.add(m);
					stack.push(m);
				}
			}
		}
		return seen;
	}

	/** Emits the pushes and, once the forward run is over, ends the workflow or starts compensation. */
	private settle(push: Set<string>): Effect[] {
		const fx = this.emit(push, false);
		if (this.order.some((n) => this.t(n).state === 'waiting' || active(this.t(n).state))) return fx;
		if (this.failed || (this.cancelled && this.undoOnCancel)) {
			const undos = this.compensate();
			if (undos.length) {
				this.state = 'compensating';
				return [...fx, ...undos];
			}
		}
		return this.end(fx, this.failed ? 'failed' : this.cancelled ? 'cancelled' : 'completed');
	}

	/** Each completed task's undo waits on the undos of all its completed descendants that have one. */
	private compensate(): Effect[] {
		const push = new Set<string>();
		for (const name of this.order) {
			const t = this.t(name);
			if (t.state !== 'completed' || !t.plan.undo) continue;
			t.undoState = 'waiting';
			t.undoOpen = 0;
			for (const d of this.reach(name, false)) {
				const dt = this.t(d);
				if (dt.state === 'completed' && dt.plan.undo) t.undoOpen++;
			}
			if (t.undoOpen === 0) {
				t.undoState = 'pending';
				push.add(name);
			}
		}
		return this.emit(push, true);
	}

	private end(fx: Effect[], state: WorkflowState): Effect[] {
		this.state = state;
		return [...fx, { end: state }];
	}

	private emit(push: Set<string>, undo: boolean): Effect[] {
		return this.order.filter((n) => push.has(n)).map((n) => (undo ? { push: n, undo } : { push: n }));
	}
}
