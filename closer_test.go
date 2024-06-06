/*
 * closer - A simple, thread-safe closer
 *
 * The MIT License (MIT)
 *
 * Copyright (c) 2019 Roland Singer <roland.singer[at]desertbit.com>
 * Copyright (c) 2019 Sebastian Borchers <sebastian[at]desertbit.com>
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package closer_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/desertbit/closer/v3"
	r "github.com/stretchr/testify/require"
)

func TestCloser_Close(t *testing.T) {
	t.Parallel()

	// Test the closer with nil errors.
	c := closer.New()
	r.False(t, c.IsClosed())
	select {
	case <-c.ClosedChan():
		t.Fatal("close chan should not read error")
	default:
	}

	// Nil error expected. Perform this two times, as we
	// expect the Close method to behave always the same,
	// regardless of how many times we call it.
	for i := 0; i < 2; i++ {
		err := c.Close()
		r.True(t, c.IsClosed())
		r.NoError(t, err)
		select {
		case <-c.ClosedChan():
		default:
			t.Fatal("close chan should read error")
		}
	}

	// Test closer with non-nil errors.
	c = closer.New()
	c.OnClose(func() error {
		return errors.New("test")
	})

	r.False(t, c.IsClosed())
	select {
	case <-c.ClosedChan():
		t.Fatal("close chan should not read error")
	default:
	}

	// Error expected. Perform this two times, as we
	// expect the Close method to behave always the same,
	// regardless of how many times we call it.
	for i := 0; i < 2; i++ {
		err := c.Close()
		r.True(t, c.IsClosed())
		r.Error(t, err)
		select {
		case <-c.ClosedChan():
		default:
			t.Fatal("close chan should read error")
		}
	}
}

func TestCloser_IsClosing(t *testing.T) {
	t.Parallel()

	p := closer.New()
	p.OnClose(func() error {
		// Check here whether the child signals that it is about to close.
		r.True(t, p.IsClosing())
		select {
		case <-p.ClosingChan():
		default:
			t.Fatal("closer should be closing")
		}
		return nil
	})
}

func TestCloser_IsClosed(t *testing.T) {
	t.Parallel()

	// One parent with a direct one-way child.
	p := closer.New()
	c := p.CloserOneWay()
	p.OnClose(func() error {
		// Check here whether the child signals that it is completely closed.
		r.True(t, c.IsClosed())
		select {
		case <-c.ClosedChan():
		default:
			t.Fatal("child closer should be closed")
		}
		return nil
	})
}

func TestCloser_WaitPanic(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic")
		}
	}()

	c := closer.New()
	c.CloserDone()
}

func TestCloser_Done(t *testing.T) {
	t.Parallel()

	c := closer.New()
	c.CloserAddWait(3)
	c.CloserDone()
	c.CloserDone()
	c.CloserDone()
	go c.Close_()

	select {
	case <-c.ClosedChan():
	case <-time.After(time.Second):
		t.Fatal("deadlock on close")
	}
}

func TestCloserErrors(t *testing.T) {
	t.Parallel()

	errSpecific := errors.New("specific error")

	c := closer.New()
	for i := 0; i < 3; i++ {
		c.OnClosing(func() error { return errors.New("error closing") })
		c.OnClose(func() error { return errors.New("error closed") })
	}
	c1 := c.CloserOneWay()
	for i := 0; i < 3; i++ {
		c1.OnClosing(func() error { return errors.New("error closing") })
		c1.OnClose(func() error { return errors.New("error closed") })
	}
	c1.OnClosing(func() error { return errSpecific })

	// Execute multiple times to test, that the error does not change between calls.
	for i := 0; i < 3; i++ {
		err := c.Close()
		r.Error(t, err)
		r.ErrorIs(t, err, errSpecific)
	}

	// Create new closer and test CloseWithErr and Error now.
	c = closer.New()
	r.Nil(t, c.CloserError())
	for i := 0; i < 3; i++ {
		c.OnClosing(func() error { return errors.New("error closing") })
		c.OnClose(func() error { return errors.New("error closed") })
	}
	// Execute multiple times to test, that the error does not change between calls.
	for i := 0; i < 3; i++ {
		c.CloseWithErr(errSpecific)
	}

	err = c.Close()
	r.Error(t, err)
	r.ErrorIs(t, err, errSpecific)
	r.Same(t, err, c.CloserError())
}

func TestCloseErrorsRace(t *testing.T) {
	t.Parallel()

	var (
		err = errors.New("error")
		c   = closer.New()
	)

	c.CloserAddWait(1)
	go func() {
		<-c.ClosingChan()
		c.CloseWithErrAndDone(err)
	}()

	c.Close()

	<-c.ClosedChan()
	r.ErrorIs(t, c.CloserError(), err)
}

func TestCloseFuncsLIFO(t *testing.T) {
	t.Parallel()

	orderChan := make(chan int, 4)

	c := closer.New()
	c.OnClose(func() error {
		orderChan <- 0
		return nil
	})
	c.OnClose(func() error {
		orderChan <- 1
		return nil
	})
	c.OnClose(func() error {
		orderChan <- 2
		return nil
	})
	c.OnClose(func() error {
		orderChan <- 3
		return nil
	})

	err := c.Close()
	r.NoError(t, err)

	for i := 3; i >= 0; i-- {
		r.Equal(t, i, <-orderChan)
	}
}

func TestCloser_Context(t *testing.T) {
	t.Parallel()

	// Test closing the closer.
	c := closer.New()
	ctx, _ := c.Context()
	c.Close_()
	time.Sleep(time.Millisecond)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("deadlock on context")
	}
}

func TestCloser_OneWay(t *testing.T) {
	// Simple test case.
	t.Run("CloseFunc", testOneWayCloseFunc)

	// Complex test case.
	t.Run("Routines", testOneWayRoutines)
}

func testOneWayCloseFunc(t *testing.T) {
	t.Parallel()

	// A closer chain with only one-way closers.
	p := closer.New()
	c1 := p.CloserOneWay()
	c2 := c1.CloserOneWay()

	// Close a child and check that the parent does not close.
	err := c2.Close()
	r.NoError(t, err)
	r.False(t, c1.IsClosing())
	r.False(t, c1.IsClosed())

	p.OnClosing(func() error {
		r.True(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.False(t, c1.IsClosing())
		r.False(t, c1.IsClosed())
		return nil
	})

	p.OnClose(func() error {
		r.True(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.True(t, c1.IsClosing())
		r.True(t, c1.IsClosed())
		return nil
	})

	// Close the parent now.
	err = p.Close()
	r.NoError(t, err)
	r.True(t, p.IsClosed())
}

func testOneWayRoutines(t *testing.T) {
	t.Parallel()

	// One parent with 2 direct, one-way children.
	p := closer.New()
	c1 := p.CloserOneWay()
	c2 := p.CloserOneWay()

	p.OnClose(func() error {
		// Both children must be closed before the parent closes.
		r.True(t, c1.IsClosed())
		r.True(t, c2.IsClosed())
		return nil
	})

	// The first child has a child of its own.
	cc1 := c1.CloserOneWay()

	c1.OnClose(func() error {
		// The child must be closed before.
		r.True(t, cc1.IsClosed())
		return nil
	})

	c1.CloserAddWait(1)
	c2.CloserAddWait(1)
	cc1.CloserAddWait(2) // Try two routines.

	// Start routines for each of the children.
	retChan := make(chan error, 4)
	f := func(c closer.Closer) {
		var err error
		select {
		case <-c.ClosingChan():
			err = c.CloseAndDone()
		case <-time.After(time.Second):
			err = errors.New("routine timed out")
		}
		retChan <- err
	}
	go f(c1)
	go f(c2)
	go f(cc1)
	go f(cc1)

	// Close the parent. This should close c1 and c2, before p closes.
	// c1 should close cc1 before it closes itself.
	err := p.Close()
	r.NoError(t, err)
	for i := 0; i < 4; i++ {
		select {
		case <-time.After(time.Second):
			t.Fatal("main timed out")
		case err = <-retChan:
			r.NoError(t, err)
		}
	}
}

func TestCloser_TwoWay(t *testing.T) {
	// Simple test case.
	t.Run("CloseFunc", testTwoWayCloseFunc)

	// Complex test case.
	t.Run("Routines", testTwoWayRoutines)

	// Test for asynchronous child close.
	t.Run("ParentWaitGroup", testTwoWayParentWaitGroup)
}

func testTwoWayCloseFunc(t *testing.T) {
	t.Parallel()

	// A closer chain with only two-way closers.
	p := closer.New()
	c1 := p.CloserTwoWay()
	c2 := c1.CloserTwoWay()

	p.OnClose(func() error {
		r.True(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.True(t, c1.IsClosing())
		r.True(t, c1.IsClosed())

		r.True(t, c2.IsClosing())
		r.True(t, c2.IsClosed())
		return nil
	})
	c1.OnClose(func() error {
		// We closed the first-level child, at least the second-level child must be closed.
		// The parent must not be closing yet.
		r.False(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.True(t, c1.IsClosing())
		r.False(t, c1.IsClosed())

		r.True(t, c2.IsClosing())
		r.True(t, c2.IsClosed())
		return nil
	})
	c2.OnClose(func() error {
		// We closed the second-level child, all others must not yet be closing.
		r.False(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.False(t, c1.IsClosing())
		r.False(t, c1.IsClosed())

		r.True(t, c2.IsClosing())
		r.False(t, c2.IsClosed())
		return nil
	})

	// Close the lowest child now.
	err := c2.Close()
	r.NoError(t, err)

	// Wait for the parent to close. This happens in a new goroutine.
	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	case <-p.ClosedChan():
	}

	// Repeat the test, but this time close the parent.
	p = closer.New()
	c1 = p.CloserTwoWay()
	c2 = c1.CloserTwoWay()

	p.OnClose(func() error {
		r.True(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.True(t, c1.IsClosing())
		r.True(t, c1.IsClosed())

		r.True(t, c2.IsClosing())
		r.True(t, c2.IsClosed())
		return nil
	})
	c1.OnClose(func() error {
		// We close the child, so the parent and the other children must close first.
		r.True(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.True(t, c1.IsClosing())
		r.False(t, c1.IsClosed())

		r.True(t, c2.IsClosing())
		r.True(t, c2.IsClosed())
		return nil
	})
	c2.OnClose(func() error {
		// We close the child, so the parent and the other children must close first.
		r.True(t, p.IsClosing())
		r.False(t, p.IsClosed())

		r.True(t, c1.IsClosing())
		r.False(t, c1.IsClosed())

		r.True(t, c2.IsClosing())
		r.False(t, c2.IsClosed())
		return nil
	})

	// Close the parent this time.
	err = p.Close()
	r.NoError(t, err)
	r.True(t, p.IsClosed())
}

func testTwoWayRoutines(t *testing.T) {
	t.Parallel()

	// One parent with 2 direct, two-way children.
	p := closer.New()
	c1 := p.CloserTwoWay()
	c2 := p.CloserTwoWay()

	p.OnClose(func() error {
		// Both children must be closed
		r.True(t, c1.IsClosed())
		r.True(t, c2.IsClosed())
		return nil
	})

	// This is a child of the first child. This closer
	// will be closed.
	cc1 := c1.CloserTwoWay()
	cc1.OnClose(func() error {
		r.False(t, p.IsClosed())
		r.False(t, c1.IsClosed())
		r.False(t, c2.IsClosed())
		return nil
	})

	c1.OnClose(func() error {
		r.True(t, cc1.IsClosed())
		r.False(t, p.IsClosed())
		r.False(t, c2.IsClosed())
		return nil
	})

	p.CloserAddWait(1)
	c1.CloserAddWait(1)
	c2.CloserAddWait(1)

	retChan := make(chan error, 4)
	f := func(c closer.Closer) {
		var err error
		select {
		case <-c.ClosingChan():
			err = c.CloseAndDone()
		case <-time.After(time.Second):
			err = errors.New("routine timed out")
		}
		retChan <- err
	}
	go f(c1)
	go f(c2)
	go f(p)

	// Close a child. This should close c1, c2 and p.
	err := c1.Close()
	r.NoError(t, err)
	for i := 0; i < 3; i++ {
		select {
		case <-time.After(time.Second):
			t.Fatal("main timed out")
		case err = <-retChan:
			r.NoError(t, err)
		}
	}
}

func testTwoWayParentWaitGroup(t *testing.T) {
	t.Parallel()

	p := closer.New()
	c := p.CloserTwoWay()

	go func() {
		p.CloserAddWait(1)
		defer p.CloseAndDone_()

		c.Close_()
	}()

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	case <-p.ClosedChan():
	}
}

func TestEndlessGrowth(t *testing.T) {
	t.Parallel()

	// Test is mainly there for 100% test coverage.

	c := closer.New()
	children := make([]closer.Closer, 0, 1000)
	for i := 0; i < 1000; i++ {
		children = append(children, c.CloserOneWay())
	}
	for i := 0; i < 1000; i++ {
		r.NoError(t, children[i].Close())
	}
}

func TestCloser_RunCloserRoutine(t *testing.T) {
	t.Parallel()

	c := closer.New()
	started := make(chan struct{})

	c.RunCloserRoutine(func() error {
		close(started)
		return errors.New("error")
	})

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	case <-started:
	}

	c.Close_()
	r.Error(t, c.CloserError())
	r.Equal(t, c.CloserError().Error(), "error")
}

func TestCloser_RunCloserRoutine_DoNotRunIfClosed(t *testing.T) {
	t.Parallel()

	c := closer.New()
	c.Close_()

	var v atomic.Bool
	c.RunCloserRoutine(func() (err error) {
		v.Store(true)
		return nil
	})

	time.Sleep(time.Second)
	r.False(t, v.Load())
}
