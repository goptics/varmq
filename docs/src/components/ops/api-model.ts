// A small model of varmq's REST handler (mux.go) and of the worker states it reaches (worker_lifecycle.go,
// worker_state.go, worker_pool.go). It runs in the page, with no server. The status codes, JSON and
// headers below were checked against the real handler: a Go program called varmq.Handler("/varmq") for
// each worker status and each route and printed the replies. Some of what http.ServeMux does on its own
// is left out: the redirect of /varmq to /varmq/, the cleaning of paths such as //, and the newline
// after each body.

export type Status = 'Idle' | 'Running' | 'Pausing' | 'Paused' | 'Stopping' | 'Stopped';

export interface Worker {
	name: string;
	status: Status;
	concurrency: number;
	processing: number;
	pending: number;
	idle: number;
	submitted: number;
	completed: number;
	successful: number;
	failed: number;
}

export interface Reply {
	code: number;
	reason: string;
	headers: [string, string][];
	body: string;
	json: boolean;
	why: string;
	/** The route answered after waiting for running jobs to end (restart). */
	waited?: boolean;
}

export const PREFIX = '/varmq';
export const INSTANCE = 'worker-a';
/** What runtime.NumCPU() returned on the machine that recorded the example for concurrency 0. */
export const CPU = 10;

const REASON: Record<number, string> = { 200: 'OK', 400: 'Bad Request', 404: 'Not Found', 405: 'Method Not Allowed', 409: 'Conflict', 500: 'Internal Server Error' };

const isActive = (w: Worker) => w.status === 'Running' || w.status === 'Idle';

/** Errors a call returns when the worker is not in a state that allows it. */
const STATUS_ERR: Record<Status, string> = {
	Idle: 'worker is already running',
	Running: 'worker is already running',
	Paused: 'worker is paused',
	Stopped: 'worker is stopped',
	Pausing: 'worker is pausing',
	Stopping: 'worker is stopping',
};

/** After a worker is free to take jobs, give it up to `concurrency` of them. */
export function pull(w: Worker) {
	const free = Math.max(0, w.concurrency - w.processing);
	const take = Math.min(free, w.pending);
	w.processing += take;
	w.pending -= take;
	if (w.status === 'Idle' || w.status === 'Running') w.status = w.processing > 0 ? 'Running' : 'Idle';
}

/** Time passes: the running jobs end. Then the worker acts on its status. */
export function tick(w: Worker) {
	w.completed += w.processing;
	w.successful += w.processing;
	w.processing = 0;
	if (w.status === 'Pausing') w.status = 'Paused';
	else if (w.status === 'Stopping') w.status = 'Stopped';
	else if (w.status === 'Running' || w.status === 'Idle') pull(w);
}

export function add(w: Worker, n: number) {
	w.submitted += n;
	w.pending += n;
	if (w.status === 'Idle' || w.status === 'Running') pull(w);
}

function reply(code: number, body: unknown, why: string, extra: Partial<Reply> = {}): Reply {
	const json = body !== null && typeof body === 'object';
	const headers: [string, string][] = [['X-Varmq-Instance', INSTANCE]];
	if (json) headers.unshift(['Content-Type', 'application/json']);
	return { code, reason: REASON[code], headers, body: json ? JSON.stringify(body) : (body as string), json, why, ...extra };
}

const plain = (code: number, text: string, why: string, allow?: string): Reply => {
	const headers: [string, string][] = [];
	if (allow) headers.push(['Allow', allow]);
	headers.push(['Content-Type', 'text/plain; charset=utf-8'], ['X-Content-Type-Options', 'nosniff'], ['X-Varmq-Instance', INSTANCE]);
	return { code, reason: REASON[code], headers, body: text, json: false, why };
};

const err = (code: number, message: string, why: string) => reply(code, { error: message }, why);
const ok = (why: string, extra: Partial<Reply> = {}): Reply => ({ ...reply(200, '', why, extra), json: false });

export const summary = (w: Worker) => ({
	name: w.name,
	status: w.status,
	concurrency: w.concurrency,
	num_processing: w.processing,
	num_pending: w.pending,
	num_idle_workers: w.idle,
});
export const detail = (w: Worker) => ({
	...summary(w),
	metrics: { submitted: w.submitted, completed: w.completed, successful: w.successful, failed: w.failed },
});

const ROUTES: { method: string; parts: string[] }[] = [
	{ method: 'GET', parts: ['health'] },
	{ method: 'GET', parts: ['workers'] },
	{ method: 'GET', parts: ['workers', '{name}'] },
	{ method: 'PATCH', parts: ['workers', '{name}', 'actions', '{action}'] },
	{ method: 'PATCH', parts: ['workers', '{name}', 'config', 'concurrency', '{number}'] },
];

// A {name} part matches one segment that is not empty, as in ServeMux: /varmq/workers/ is no route.
function match(parts: string[]) {
	return ROUTES.filter((r) => r.parts.length === parts.length && r.parts.every((p, i) => (p.startsWith('{') ? parts[i] !== '' : p === parts[i])));
}

// strconv.Atoi on a 64-bit build: an optional sign, then digits, within int64.
const INT_MIN = -(2n ** 63n);
const INT_MAX = 2n ** 63n - 1n;
const atoi = (s: string): number | null => {
	if (!/^[+-]?\d+$/.test(s)) return null;
	const n = BigInt(s);
	return n < INT_MIN || n > INT_MAX ? null : Number(n);
};

/** withSafeConcurrency in config.go: below 1 is the CPU count, and the limit is clamped to MaxUint32. */
const MAX_UINT32 = 4294967295;
const safeConcurrency = (n: number) => (n < 1 ? CPU : Math.min(n, MAX_UINT32));

/** Handle one request against the workers, the way varmq.Handler(PREFIX) does. Mutates the workers. */
export function handle(workers: Map<string, Worker>, method: string, path: string): Reply {
	// The outer mux of the program sends only PREFIX + "/" to the handler.
	if (!path.startsWith(PREFIX + '/')) {
		return { code: 404, reason: REASON[404], headers: [['Content-Type', 'text/plain; charset=utf-8'], ['X-Content-Type-Options', 'nosniff']], body: '404 page not found', json: false, why: `Your mux sends only ${PREFIX}/ to varmq.Handler, so this path never reaches it, and the reply has no X-Varmq-Instance header.` };
	}
	const parts = path.slice(PREFIX.length).split('/').filter((p, i) => !(i === 0 && p === ''));
	const hit = match(parts);
	if (!hit.length) return plain(404, '404 page not found', 'The mux knows no route for this path. Handler adds the instance header to this reply too.');
	const route = hit.find((r) => r.method === method);
	if (!route) {
		const allow = hit[0].method === 'GET' ? 'GET, HEAD' : hit[0].method;
		return plain(405, 'Method Not Allowed', `The path exists, but only for ${allow}. The mux answers with an Allow header.`, allow);
	}

	const [a, name, b, c] = [parts[0], parts[1], parts[2], parts[3]];
	if (a === 'health') return reply(200, { status: 'ok' }, 'handleHealthCheck always answers ok.');
	if (a === 'workers' && parts.length === 1) return reply(200, Array.from(workers.values()).map(summary), 'A summary of every registered worker, in the order the registry map yields them, which is not defined.');

	const w = workers.get(name);
	if (!w) return err(404, 'worker not found', 'No registered worker has this name. A worker with no name is never registered.');
	if (parts.length === 2) return reply(200, detail(w), 'One worker with its four counters.');

	if (b === 'actions') {
		switch (c) {
			case 'pause':
				if (w.status === 'Pausing' || w.status === 'Paused') return ok('Already pausing or paused: Pause returns nil, so the reply is 200 and nothing changes.');
				if (w.status === 'Idle') {
					w.status = 'Paused';
					return ok('Nothing was running, so the worker is Paused at once.');
				}
				if (w.status === 'Running') {
					w.status = 'Pausing';
					return ok('Pause returns at once. The worker is Pausing until its running jobs end, then Paused.');
				}
				return err(500, STATUS_ERR[w.status], `Pause on a ${w.status} worker returns ErrWorker${w.status === 'Stopped' ? 'Stopped' : 'Stopping'}. It is not one of the three 409 errors, so it is a 500.`);
			case 'resume':
				if (w.status === 'Paused') {
					w.status = 'Idle';
					pull(w);
					return ok('A Paused worker goes back to Idle, and takes waiting jobs.');
				}
				if (isActive(w)) return ok('The worker is already active: Resume returns nil, so the reply is 200 and nothing changes.');
				return err(500, STATUS_ERR[w.status], `Resume on a ${w.status} worker returns ErrWorker${w.status}. That is a 500. Resume works once the worker reads Paused.`);
			case 'start':
				if (isActive(w)) return err(409, 'worker is already running', 'Start returns ErrRunningWorker, one of the three errors that map to 409.');
				if (w.status === 'Stopped') {
					w.status = 'Idle';
					pull(w);
					return ok('A Stopped worker starts again and takes waiting jobs.');
				}
				return err(500, STATUS_ERR[w.status], `Start accepts only an initiated or stopped worker. On a ${w.status} one it returns ErrWorker${w.status}, a 500.`);
			case 'stop':
				if (w.status === 'Stopped' || w.status === 'Stopping') return ok('Already stopping or stopped: Stop returns nil, so the reply is 200 and nothing changes.');
				if (w.processing === 0) {
					w.status = 'Stopped';
					return ok('Stop returns at once. Nothing was running, so the worker reads Stopped a moment later, when its event loop has stopped.');
				}
				w.status = 'Stopping';
				return ok('Stop returns at once. The worker is Stopping until its running jobs end, then Stopped. Jobs that have not started stay queued.');
			case 'restart': {
				const waited = w.processing > 0;
				w.completed += w.processing;
				w.successful += w.processing;
				w.processing = 0;
				w.status = 'Idle';
				pull(w);
				return ok(waited ? 'Restart stops the worker, waits until it has stopped, and starts it. The reply came after the running jobs ended.' : 'Restart stopped the worker and started it again. Nothing was running, so it was quick.', { waited });
			}
			default:
				return err(400, 'unknown action', 'The action is not one of pause, resume, restart, start and stop.');
		}
	}

	// /workers/{name}/config/concurrency/{number}
	const asked = atoi(parts[4]);
	if (asked === null) return err(400, 'invalid concurrency value', 'The number is not an integer that fits in an int, so strconv.Atoi fails and varmq answers before it calls TunePool.');
	if (!isActive(w)) return err(500, STATUS_ERR[w.status], `TunePool needs an active worker. On a ${w.status} one it returns ErrWorker${w.status}, a 500.`);
	const safe = safeConcurrency(asked);
	if (safe === w.concurrency) return err(409, 'worker already has the same concurrency', 'TunePool returns ErrSameConcurrency, one of the three errors that map to 409.');
	const raised = safe > w.concurrency;
	w.concurrency = safe;
	if (raised) pull(w);
	return ok(
		asked < 1
			? `A number below 1 becomes the CPU count (${CPU} on the machine that recorded this).`
			: safe < asked
				? `The limit went up. varmq clamps it to ${MAX_UINT32}, the largest uint32.`
				: raised
					? 'The limit went up, so the worker takes more waiting jobs now.'
					: 'The limit went down. Jobs already running keep running.',
	);
}

export const seed = (): Map<string, Worker> =>
	new Map([
		['email-sender', { name: 'email-sender', status: 'Running', concurrency: 2, processing: 2, pending: 14, idle: 0, submitted: 20, completed: 4, successful: 4, failed: 0 }],
		['image-processor', { name: 'image-processor', status: 'Running', concurrency: 4, processing: 4, pending: 8, idle: 0, submitted: 20, completed: 8, successful: 8, failed: 0 }],
	]);
