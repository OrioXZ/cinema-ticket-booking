package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Runner func(context.Context, func()) error

type Worker struct {
	Name string
	Run  Runner
}

type WorkerError struct {
	Name string
	Err  error
}

type Group struct {
	cancel context.CancelFunc
	wait   sync.WaitGroup
	errors chan WorkerError
}

func Start(parent context.Context, timeout time.Duration, workers []Worker) (*Group, error) {
	ctx, cancel := context.WithCancel(parent)
	group := &Group{
		cancel: cancel,
		errors: make(chan WorkerError, len(workers)),
	}
	ready := make(chan string, len(workers))

	for _, worker := range workers {
		worker := worker
		group.wait.Add(1)
		go func() {
			defer group.wait.Done()
			var readyOnce sync.Once
			err := worker.Run(ctx, func() {
				readyOnce.Do(func() {
					ready <- worker.Name
				})
			})
			if err == nil && ctx.Err() == nil {
				err = errors.New("worker stopped unexpectedly")
			}
			if err != nil && ctx.Err() == nil {
				group.errors <- WorkerError{Name: worker.Name, Err: err}
			}
		}()
	}

	allStopped := make(chan struct{})
	go func() {
		group.wait.Wait()
		close(group.errors)
		close(allStopped)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	readyWorkers := make(map[string]struct{}, len(workers))
	for len(readyWorkers) < len(workers) {
		select {
		case name := <-ready:
			readyWorkers[name] = struct{}{}
		case workerError, ok := <-group.errors:
			if !ok {
				cancel()
				<-allStopped
				return nil, errors.New("workers stopped before becoming ready")
			}
			cancel()
			<-allStopped
			return nil, fmt.Errorf("%s failed before readiness: %w", workerError.Name, workerError.Err)
		case <-timer.C:
			cancel()
			<-allStopped
			return nil, fmt.Errorf("worker readiness timed out after %s", timeout)
		case <-parent.Done():
			cancel()
			<-allStopped
			return nil, parent.Err()
		}
	}

	return group, nil
}

func (g *Group) Errors() <-chan WorkerError {
	return g.errors
}

func (g *Group) Stop() {
	g.cancel()
}

func (g *Group) Wait() {
	g.wait.Wait()
}
