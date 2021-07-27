package util

import (
	"context"
	"testing"
	"time"
)

func TestDummyThreadpool(t *testing.T) {
	tp := NewDummyThreadPool()

	dummyFunc := func(ctx context.Context) {}

	err := tp.SubmitFuncUnsafe(dummyFunc)
	if err == nil {
		t.Errorf("Error was Not Reached when submitting to dummy threadpool\n")
	}

	err = tp.Finish(time.Now())
	if err == nil {
		t.Errorf("Error was Not Reached when submitting to dummy threadpool!\n")
	}
}

func TestClosedThreadpool(t *testing.T) {
	tp := NewThreadPool(0)

	err := tp.Finish(time.Now())
	if err != nil {
		t.Errorf("Error reached closing empty threadpool. Err: %v\n", err)
	}

	dummyFunc := func(ctx context.Context) {}

	err = tp.SubmitFuncUnsafe(dummyFunc)
	if err == nil {
		t.Errorf("Error was Not Reached when submitting to closed threadpool!\n")
	}

	err = tp.Finish(time.Now())
	if err == nil {
		t.Errorf("Error was Not Reached when submitting to closed threadpool!\n")
	}
}

func TestEmptyThreadpool(t *testing.T) {
	tp := NewThreadPool(0)

	dummyFunc := func(ctx context.Context) {}

	err := tp.SubmitFuncUnsafe(dummyFunc)
	if err == nil {
		t.Errorf("Error was Not Reached when submitting to Empty threadpool!\n")
	}

	err = tp.Finish(time.Now())
	if err != nil {
		t.Errorf("Error was Reached when submitting to Empty threadpool!\n")
	}
}

func TestThreadPool(t *testing.T) {
	// Should be held constant
	threads := 20

	tp := NewThreadPool(threads / 2)
	channelIDsIn := make(chan int, threads)
	channelIDsOut := make(chan int, threads)
	channelDone := make(chan bool, 1)

	err := tp.SubmitFuncUnsafe(
		func(c context.Context) {
			var iD int
			var iDSet map[int]bool = make(map[int]bool)
			for i := 0; i < threads; i++ {
				select {
				case iD = <-channelIDsOut:
					t.Logf("ID Found %d!\n", iD)
					iDSet[iD] = true
					break
				case <-c.Done():
					return
				default:
				}
			}

			channelDone <- len(iDSet) == threads
		})

	if err != nil {
		t.Errorf("Error reached when submitting Function to threadpool: %v\n", err)
	}

	for i := 0; i < threads; i++ {
		channelIDsIn <- i
		tp.SubmitFuncBlock(func(c context.Context) {
			derp := <-channelIDsIn
			channelIDsOut <- derp
		})
	}

	finished := <-channelDone
	if !finished {
		t.Errorf("ThreadPool Did Not Give Back Unique IDs!\n")
	}
}

func TestLongRunningCancel(t *testing.T) {
	tp := NewThreadPool(1)
	channel := make(chan bool)

	tp.SubmitFuncBlock(func(c context.Context) {
		<-channel
	})

	err := tp.Finish(time.Now().Add(time.Millisecond))
	if err == nil {
		t.Errorf("Threadpool did not warn of running thread!\n")
	}

	t.Logf("Expected Error was Reached! YAY! Err: %v\n", err)

}
