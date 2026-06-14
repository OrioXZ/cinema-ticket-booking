package lifecycle

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartWaitsForAllWorkers(t *testing.T) {
	gates := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	workers := make([]Worker, 0, len(gates))
	for index, gate := range gates {
		gate := gate
		workers = append(workers, Worker{
			Name: string(rune('a' + index)),
			Run: func(ctx context.Context, ready func()) error {
				select {
				case <-gate:
					ready()
				case <-ctx.Done():
					return nil
				}
				<-ctx.Done()
				return nil
			},
		})
	}

	result := make(chan *Group, 1)
	failures := make(chan error, 1)
	go func() {
		group, err := Start(context.Background(), time.Second, workers)
		if err != nil {
			failures <- err
			return
		}
		result <- group
	}()

	close(gates[0])
	close(gates[1])
	select {
	case <-result:
		t.Fatal("startup completed before all workers were ready")
	case err := <-failures:
		t.Fatalf("startup failed early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(gates[2])

	var group *Group
	select {
	case group = <-result:
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("startup did not complete")
	}
	group.Stop()
	group.Wait()
}

func TestWorkerErrorBeforeReadinessFailsStartup(t *testing.T) {
	want := errors.New("subscribe failed")
	_, err := Start(context.Background(), time.Second, []Worker{{
		Name: "audit",
		Run: func(context.Context, func()) error {
			return want
		},
	}})
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "audit") {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestReadinessSignalCountsOnlyOncePerWorker(t *testing.T) {
	group, err := Start(context.Background(), time.Second, []Worker{
		{
			Name: "first",
			Run: func(ctx context.Context, ready func()) error {
				ready()
				ready()
				<-ctx.Done()
				return nil
			},
		},
		{
			Name: "second",
			Run: func(ctx context.Context, ready func()) error {
				ready()
				<-ctx.Done()
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	group.Stop()
	group.Wait()
}

func TestStartupTimeoutCancelsWorkers(t *testing.T) {
	canceled := make(chan struct{})
	_, err := Start(context.Background(), 30*time.Millisecond, []Worker{{
		Name: "blocked",
		Run: func(ctx context.Context, _ func()) error {
			<-ctx.Done()
			close(canceled)
			return nil
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("timed-out worker was not canceled and joined")
	}
}

func TestContextCancellationStopsAllWorkers(t *testing.T) {
	var stopped atomic.Int32
	workers := make([]Worker, 3)
	for index := range workers {
		workers[index] = Worker{
			Name: string(rune('a' + index)),
			Run: func(ctx context.Context, ready func()) error {
				ready()
				<-ctx.Done()
				stopped.Add(1)
				return nil
			},
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	group, err := Start(ctx, time.Second, workers)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	group.Wait()
	if stopped.Load() != 3 {
		t.Fatalf("stopped workers = %d, want 3", stopped.Load())
	}
}
