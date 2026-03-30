package csync

import (
	"context"
	"sync"
)

// WaitWithContext blocks until wg is done or ctx is cancelled. It returns
// true when all goroutines completed, false when the context fired first.
func WaitWithContext(ctx context.Context, wg *sync.WaitGroup) bool {
	ch := make(chan struct{}, 1)
	go func() { wg.Wait(); ch <- struct{}{} }()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}
