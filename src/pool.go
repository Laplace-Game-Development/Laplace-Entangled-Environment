package main

import (
	"context"
	"errors"
	"time"
)

// Todo we may want to make this it's own package

type ThreadPool struct {
	threadNum       int
	unusedResources chan int
	context         context.Context
	cancel          context.CancelFunc
	closed          bool
}

func NewDummyThreadPool() ThreadPool {
	return ThreadPool{closed: true}
}

func NewThreadPool(numberOfThreads int) ThreadPool {
	return NewThreadPoolWithContext(numberOfThreads, context.Background())
}

func NewThreadPoolWithContext(numberOfThreads int, outerContext context.Context) ThreadPool {
	ctx, withCancel := context.WithCancel(outerContext)

	pool := ThreadPool{
		threadNum:       numberOfThreads,
		unusedResources: make(chan int, numberOfThreads),
		context:         ctx,
		cancel:          withCancel,
		closed:          false,
	}

	for i := 0; i < numberOfThreads; i++ {
		pool.unusedResources <- i + 1
	}

	return pool
}

func (tp ThreadPool) SubmitFuncUnsafe(fun func(context.Context)) error {
	if tp.closed {
		return errors.New("Thread Pool is already closed!")
	}

	var resource int
	select {
	case resource = <-tp.unusedResources:
		go func() {
			fun(tp.context)
			tp.unusedResources <- resource
		}()

		return nil
	default:
		return errors.New("No Available Resources!")
	}
}

func (tp ThreadPool) SubmitFuncBlock(fun func(context.Context)) error {
	if tp.closed {
		return errors.New("Thread Pool is already closed!")
	}

	resource := <-tp.unusedResources

	go func() {
		fun(tp.context)
		tp.unusedResources <- resource
	}()

	return nil
}

func (tp ThreadPool) Finish(deadline time.Time) error {
	if tp.closed {
		return errors.New("Thread Pool is already closed!")
	}
	tp.closed = true

	i := 0

	for i < tp.threadNum {
		select {
		case <-tp.unusedResources:
			i += 1
			continue
		default:
		}

		if time.Now().Before(deadline) {
			continue
		}

		tp.cancel()
		break
	}

	for i < tp.threadNum {
		select {
		case <-tp.unusedResources:
			i += 1
		default:
			return errors.New("Could Not Cleanup Thread Pool!")
		}
	}

	return nil
}
