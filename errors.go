package gocq

import "errors"

var ErrWorkerAlreadyBound = errors.New("the worker is already bound to a queue")

func selectError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
