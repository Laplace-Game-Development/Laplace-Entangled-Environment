package redis

import (
	"context"
	"testing"
)

// https://blog.alexellis.io/golang-writing-unit-tests/
// https://medium.com/@benbjohnson/structuring-tests-in-go-46ddee7a25c

func TestStartHTTPListening(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	//startHTTPListening(ctx)
}
