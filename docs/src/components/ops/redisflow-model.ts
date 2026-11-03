// The state of the Redis scene and what each button does to it. The rules come from the Redis page and
// redisq: one script moves a taken job from the queue to <key>:nacked with a deadline in <key>:timeouts
// (visibility timeout, 5 minutes by default); Acknowledge removes both; every queue object sweeps once a
// second and puts back jobs whose deadline passed or whose holder's heartbeat is 30 seconds old; a
// distributed consumer is taken for dead about six seconds after the crash; Close hands jobs back at once.

export type Mode = 'persistent' | 'dist';

export const VISIBILITY = 300; // seconds
export const HEARTBEAT = 30;
export const DIST_BACK = 6;

export interface State {
	mode: Mode;
	jobs: number[];
	inflight: number | null;
	closedUnder: boolean;
	crashed: boolean;
	t: number;
	returned: boolean; // the sweep has put the job back
	runs: Record<number, number>;
	msg: string;
}

export const fresh = (mode: Mode): State => ({
	mode,
	jobs: [7, 8, 9, 10],
	inflight: null,
	closedUnder: false,
	crashed: false,
	t: 0,
	returned: false,
	runs: {},
	msg: 'Job 7 waits in jobs. Press Take a job to see what one script does to the keys.',
});

export const mmss = (sec: number) => `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, '0')}`;
const threshold = (s: State) => (s.mode === 'dist' ? DIST_BACK : HEARTBEAT);
/** A live queue object took the job and has neither acknowledged it nor handed it back. */
export const held = (s: State) => s.inflight !== null && !s.closedUnder && !s.returned;
/** The job is in jobs:nacked: held by a live queue object, or left there by one that died. */
export const nacked = (s: State) => held(s) || (s.crashed && s.inflight !== null && !s.returned);

export function take(s: State) {
	s.inflight = s.jobs.shift()!;
	s.runs[s.inflight] = (s.runs[s.inflight] ?? 0) + 1;
	s.returned = false;
	s.closedUnder = false;
	s.msg = `One script moved job ${s.inflight} from jobs to jobs:nacked under ack-${s.inflight}, and set its deadline in jobs:timeouts: Redis’s time plus the visibility timeout, 5 minutes by default.`;
}

export function ack(s: State) {
	const id = s.inflight!;
	if (s.closedUnder) {
		s.msg = `Acknowledge returned false: the queue was closed, so the worker reports ErrAcknowledgeJob on Errs(). The function of job ${id} had run, and job ${id} is back in jobs, so it runs again.`;
		s.inflight = null;
		s.closedUnder = false;
	} else {
		s.msg = `Acknowledge removed the entry from jobs:nacked and from jobs:timeouts. Redis forgot job ${id}.`;
		s.inflight = null;
	}
}

export function kill(s: State) {
	s.crashed = true;
	s.t = 0;
	if (s.closedUnder) {
		// Close already moved the job from jobs:nacked back to the front of jobs: the dead process holds nothing.
		s.msg = `The process died. Close had already put job ${s.inflight} back at the front of jobs, so nothing is held, and the job runs again.`;
		s.inflight = null;
		s.closedUnder = false;
		return;
	}
	s.msg =
		s.inflight === null
			? 'The process died while no job was running. Nothing is held. The jobs wait in jobs.'
			: `The process died. Job ${s.inflight} stays in jobs:nacked with its deadline. Move the slider to move time forward.`;
}

export function close(s: State) {
	s.closedUnder = true;
	s.jobs.unshift(s.inflight!);
	s.msg = `Close put job ${s.inflight} back at the front of jobs at once, while its function still runs. After Close, Acknowledge on that object returns false. Stop the worker before you close the queue.`;
}

/** Time moved to `s.t`: a sweep puts the job back once the dead holder is taken for dead. */
export function sweepIfDue(s: State) {
	if (!s.crashed || s.inflight === null || s.returned) return;
	if (s.t >= threshold(s)) {
		s.returned = true;
		s.jobs.unshift(s.inflight);
		s.inflight = null;
		s.crashed = false; // a consumer is alive again: the program restarted, or consumer B
		s.msg =
			s.mode === 'dist'
				? 'About six seconds after the crash, Redis has dropped the dead consumer’s wake-up channel, and consumer B’s sweep puts job 7 back at the front of jobs and wakes a consumer for it.'
				: `The restarted program has run for ${HEARTBEAT} seconds. A queue object’s sweep finds the dead object’s heartbeat that old, and puts job 7 back at the front of jobs. A job past its deadline comes back as soon as any queue object finds it, whichever comes first.`;
	} else {
		s.msg =
			s.mode === 'dist'
				? 'Job 7 is still in jobs:nacked. The other consumers wait for the dead one’s channel to go.'
				: `Job 7 is still in jobs:nacked. The heartbeat is ${s.t} seconds old and the limit is ${HEARTBEAT}. The deadline is in ${mmss(Math.max(0, VISIBILITY - s.t))}.`;
	}
}
