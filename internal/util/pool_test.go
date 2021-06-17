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
		t.Errorf("Error was Not Reached when submitting to dummy threadpool!\n")
	}

	err = tp.Finish(time.Now())
	if err == nil {
		t.Errorf("Error was Not Reached when submitting to dummy threadpool!\n")
	}
}
