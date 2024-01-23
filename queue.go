package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/sasha-s/go-deadlock"
)

type Queue struct {
	dir     string
	cond    *sync.Cond
	maxStep int64
}

const bucketName = "steps"

func newQueue() (*Queue, error) {
	dir, err := os.MkdirTemp("", "queue")
	if err != nil {
		return nil, err
	}

	lock := new(deadlock.Mutex)
	cond := sync.NewCond(lock)

	return &Queue{
		dir:  dir,
		cond: cond,
	}, nil
}

func (q *Queue) path(step int64) string {
	return filepath.Join(q.dir, fmt.Sprintf("%d.html", step))
}

func (q *Queue) putStep(step int64, event []byte) error {
	q.cond.L.Lock()
	if step > q.maxStep {
		q.maxStep = step
	}
	q.cond.L.Unlock()

	// dumb cleanup
	go func() {
		oldstep := step - 1000
		p := q.path(oldstep)
		os.Remove(p)
	}()

	return os.WriteFile(q.path(step), event, 0644)
}

func (q *Queue) getStep(step int64) ([]byte, error) {
	return os.ReadFile(q.path(step))
}

func (q *Queue) PushStep(step int64, event []byte) error {
	if err := q.putStep(step, event); err != nil {
		return err
	}

	q.cond.Broadcast()

	return nil
}

func (q *Queue) Steps(ctx context.Context, currentStep int64) <-chan []byte {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
		nextStep := currentStep + 1
		for {
			for {
				q.cond.L.Lock()
				maxStep := q.maxStep
				q.cond.L.Unlock()

				if maxStep < nextStep {
					break
				}

				event, err := q.getStep(nextStep)
				if err != nil {
					log.Printf("error in getStep: %s (subscriber exited)\n", err.Error())
					return
				}
				nextStep++

				select {
				case <-ctx.Done():
					return
				case ch <- event:
				}
			}

			q.cond.L.Lock()
			for q.maxStep < nextStep {
				q.cond.Wait()
				if ctx.Err() != nil {
					q.cond.L.Unlock()
					return
				}
			}
			q.cond.L.Unlock()
		}
	}()

	return ch
}

func (q *Queue) DeleteAllSteps() error {
	return os.RemoveAll(q.dir)
}
