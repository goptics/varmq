// The five steps of the crash test and varmq's call at each one, for each place the queue can live.
// In memory there is no acknowledgement: the worker takes the job with Dequeue and closes it when the
// function returns (source.go, worker_pool.go). A worker whose only source is redisq or sqliteq takes
// its jobs with DequeueBatchWithAckIds, one call for all its free slots (worker_credit.go). An adapter
// of your own needs only DequeueWithAckId.

export type Mode = 'memory' | 'redis' | 'dist' | 'sqlite' | 'own';

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
			{ label: 'Waiting', sub: 'Add', call: 'queue.Add(job)  // the job value, kept in memory' },
			{ label: 'Taken', sub: 'Dequeue', call: 'item, ok := q.Dequeue()  // no acknowledgement ID' },
			{ label: 'Running', sub: 'fn(job)', call: 'process(job)  // your function runs' },
			{ label: 'Returned', sub: 'Close', call: 'job.Close()  // in memory there is nothing to acknowledge' },
			{ label: 'Done', sub: 'Closed', call: '// the job is closed: nothing outside the process knew of it' },
		];
	}
	const take =
		mode === 'own'
			? { sub: 'DequeueWithAckId', call: 'item, ok, ackID := q.DequeueWithAckId()' }
			: { sub: 'DequeueBatchWithAckIds', call: 'n, remaining, err := q.DequeueBatchWithAckIds(items, ackIDs)  // a job and an ack ID per free slot' };
	return [
		{ label: 'Waiting', sub: 'Add', call: 'queue.Add(job)  // stored as JSON in the storage' },
		{ label: 'Taken', ...take },
		{ label: 'Running', sub: 'fn(job)', call: 'process(job)  // your function runs' },
		{ label: 'Returned', sub: 'Acknowledge', call: 'q.Acknowledge(ackID)  // after the function returned' },
		{ label: 'Acknowledged', sub: 'true', call: '// Acknowledge returned true: the storage forgets the job' },
	];
}
