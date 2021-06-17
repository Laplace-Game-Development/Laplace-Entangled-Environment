// The util package represents common utility functions with no project references. These
// functions may be used freely across the project without dependency. This module hopes to
// limit the amount of duplicated code for the codebase. Hence they provide "utility"
// functionality.
package util

import (
	"context"
	"errors"
	"time"
)

// A ThreadPool represents a pipeline implementation to limit the number of goroutines
// for a set of given functions. Functions should be used to initialize the structure
// correctly. Then functions may be submitted to be synchronously added to the pipeline
// and be consumed asynchronously. Threadpools use channels (hopefully well) to be
// threadsafe. I would suggest committing to another package rather than my nooby code.
type ThreadPool struct {
	threadNum       int
	unusedResources chan int
	context         context.Context
	cancel          context.CancelFunc
	closed          bool
}

// A Dummy ThreadPool may be useful for testing. It creates a ThreadPool which starts off
// closed. It will simply reject tasks upon submission.
func NewDummyThreadPool() ThreadPool {
	return ThreadPool{closed: true}
}

// Constructs a new threadpool with a predefined "number of threads." This represents the
// numerical limit of goroutines this object can launch. The constraints of this can be
// described in later functions (see SubmitFuncUnsafe and SubmitFuncBlock).
//
// The Threadpool will also default to the background context since none is provided.
func NewThreadPool(numberOfThreads int) ThreadPool {
	return NewThreadPoolWithContext(numberOfThreads, context.Background())
}

// Constructs a new threadpool with a predefined "number of threads." This represents the
// numerical limit of goroutines this object can launch. The constraints of this can be
// described in later functions (see SubmitFuncUnsafe and SubmitFuncBlock).
//
// The Threadpool will also construct a context with cancel off of the provided context. The
// provided context should not be empty.
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

// Adds a function to the threadpool. The function will be consumed by a goroutine
// if available (is running less goroutines than the "number of threads").
// Otherwise it will return an error. Additionally, the function will
// be provided with the context of the threadpool. The function should exit
// immediately if the context is "done". (See context.Context in Golang Docs)
//
// If the threadpool is closed, the threadpool will also return an error, rejecting
// the function.
func (tp *ThreadPool) SubmitFuncUnsafe(fun func(context.Context)) error {
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

// Adds a function to the threadpool. The function will be consumed by a goroutine
// if available (is running less goroutines than the "number of threads").
// Otherwise it will "block" until one is. Additionally, the function
// will be provided with the context of the threadpool. The function should exit
// immediately if the context is "done". (See context.Context in Golang Docs)
//
// If the threadpool is closed, the threadpool will also return an error, rejecting
// the function.
//
// The term "blocking" means that the calling "thread" or runtime will be paused to
// handle other code (i.e. other functions submitted to the thread pool.) If this is
// not viable (i.e. you need to respond to the client) then consider SubmitFuncUnsafe
func (tp *ThreadPool) SubmitFuncBlock(fun func(context.Context)) error {
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

// The threadpool will close. If it is already closed then this call will result in
// error.
//
// The thread pool consumes any resources not already consumed by other goroutines.
// It continues this until the provided deadline. It will then cancel the context
// and try one last time to consume any resources.
// If no resources can be found it will respond an error.
func (tp *ThreadPool) Finish(deadline time.Time) error {
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
