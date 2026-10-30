// The steps of the crash test and varmq's call at each one, for each place the queue can live.
// In memory there is no acknowledgement: the worker takes the job with Dequeue and closes it when the
// function returns (source.go, worker_pool.go). A worker whose only source is redisq or sqliteq takes
// its jobs with DequeueBatchWithAckIds, one call for up to 64 free slots (maxCredit, worker_credit.go).
// An adapter of your own needs only DequeueWithAckId. Closing a job calls Acknowledge, so between the
// function's return and the storage's answer the job has run but is not acknowledged (job.go).

export type Mode = 'memory' | 'redis' | 'sqlite' | 'own';

export interface Step {
	label: string;
	/** The call under the step's name. */
	sub: string;
	/** The Go call strip while the job is at this step. */
	call: string;
}

export function stepsOf(mode: Mode): Step[] {
	if (mode === 'memory') {
		return [
			{ label: 'Waiting', sub: 'Add', call: 'queue.Add(job)  // kept in memory' },
			{ label: 'Taken', sub: 'Dequeue', call: 'item, ok := q.Dequeue()  // no acknowledgement ID' },
			{ label: 'Running', sub: 'fn(job)', call: 'process(job)' },
			{ label: 'Closed', sub: 'Close', call: 'job.Close()  // nothing to acknowledge' },
		];
	}
	const take =
		mode === 'own'
			? { sub: 'DequeueWithAckId', call: 'item, ok, ackID := q.DequeueWithAckId()' }
			: { sub: 'DequeueBatchWithAckIds', call: 'q.DequeueBatchWithAckIds(items, ackIDs)' };
	return [
		{ label: 'Waiting', sub: 'Add', call: 'queue.Add(job)  // stored outside the process' },
		{ label: 'Taken', ...take },
		{ label: 'Running', sub: 'fn(job)', call: 'process(job)' },
		{ label: 'Returned', sub: 'not acknowledged', call: 'job.Close()  // calls q.Acknowledge(ackID) next' },
		{ label: 'Acknowledged', sub: 'Acknowledge', call: 'q.Acknowledge("ack-7")  // true: the storage drops job 7' },
	];
}
