package varmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"weak"
)

// taskEnd is the TaskEnd Close reports for a tagged job: how its run ended,
// or cancelled when it never ran (Purge, a Close before it ran).
func taskEnd(ext *ackExt) TaskEnd {
	e := TaskEnd{Outcome: OutcomeCancelled}
	if ext.end != nil {
		e = *ext.end
	}
	t := ext.task
	e.Workflow, e.Task, e.Undo = t.wf, t.task, t.undo

	return e
}

// finishMemTask reports an in-memory task's end to its store, which pushes
// what it releases. A tag with no store (never submitted) reports nothing.
// ponytail: the store errs only for a workflow it no longer holds (swept,
// so already ended) or a tag not from its plan; the job closes anyway.
func finishMemTask(ext *ackExt) {
	if m, ok := ext.task.store.(*memWorkflowStore); ok {
		_ = m.finish(taskEnd(ext))
	}
}

// runnable is the pre-run check: false when j's workflow says its task may
// not start (cancelled, ended, or gone: ErrWorkflowNotFound), or a durable
// task has no registered store (reported; held, not acked). A plain job, or
// an in-memory tag with no store, runs with no I/O. Any other store error runs the
// task: it is at-least-once anyway, and a store that can't answer could not
// finish it either.
// ponytail: context.Background, not the worker's: Stop cancels that while
// the first job of a batch still runs; a hung store hangs the ack too.
func (w *worker[T, JobType]) runnable(j JobType) bool {
	t := j.taskTag()
	if t == nil {
		return true
	}
	if t.store == nil {
		if t.scope == "" { // in-memory, never submitted
			return true
		}
		// Durable task, no store: never run it unchecked. The pool holds it.
		w.sendError(fmt.Errorf("varmq: no workflow store for scope %s: create the storage's workflow store (for example redisq.NewWorkflowStore)", t.scope))
		return false
	}

	ok, err := t.store.Runnable(context.Background(), t.wf, t.task, t.undo)
	if errors.Is(err, ErrWorkflowNotFound) {
		return false
	}
	if err != nil {
		w.sendError(fmt.Errorf("varmq: workflow %s task %s: pre-run check: %w", t.wf, t.task, err))
		return true
	}

	return ok
}

// queuePoker wakes one worker bound to a queue: its source's ready hint,
// then its event loop.
type queuePoker struct {
	id   string
	poke func()
}

// localPokers holds, per Queue ID, the weak pokers of this process's
// workers bound to that queue: FinishTask names the queues it pushed into,
// and only subscribable storage wakes its consumers itself. The worker
// holds each poker (pokers), so binding never keeps a worker alive.
// ponytail: an emptied per-ID set stays, as queueIDs does.
var localPokers sync.Map // queueID → *sync.Map[weak.Pointer[queuePoker]]struct{}

// addPoker registers src of queue id with localPokers.
func (w *worker[T, JobType]) addPoker(id string, src source[JobType]) {
	poke := pokeOf(src)
	p := &queuePoker{id: id, poke: func() {
		if poke != nil {
			poke()
		}
		w.notifyToPullNextJobs()
	}}
	w.pokers.Store(src, p)

	set, _ := localPokers.LoadOrStore(id, &sync.Map{})
	set.(*sync.Map).Store(weak.Make(p), struct{}{})
}

// dropPoker unregisters src's poker, if it has one.
func (w *worker[T, JobType]) dropPoker(src source[JobType]) {
	v, ok := w.pokers.LoadAndDelete(src)
	if !ok {
		return
	}

	p := v.(*queuePoker)
	if set, ok := localPokers.Load(p.id); ok {
		set.(*sync.Map).Delete(weak.Make(p))
	}
}

// pokeLocal wakes this process's workers bound to the queues ids, dropping
// pokers of collected workers.
func pokeLocal(ids []string) {
	for _, id := range ids {
		set, ok := localPokers.Load(id)
		if !ok {
			continue
		}

		set.(*sync.Map).Range(func(k, _ any) bool {
			if p := k.(weak.Pointer[queuePoker]).Value(); p != nil {
				p.poke()
			} else {
				set.(*sync.Map).Delete(k)
			}
			return true
		})
	}
}
