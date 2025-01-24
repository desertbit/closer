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

package closer

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	r "github.com/stretchr/testify/require"
)

func TestCloser_Close(t *testing.T) {
	t.Parallel()

	// Test the closer with nil errors.
	c := New()
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
	c = New()
	Hook(c, func(h H) {
		h.OnCloseWithErr(func() error {
			return errors.New("test")
		})
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

	p := New()
	Hook(p, func(h H) {
		h.OnClose(func() {
			// Check here whether the child signals that it is about to close.
			r.True(t, p.IsClosing())
			select {
			case <-p.ClosingChan():
			default:
				t.Fatal("closer should be closing")
			}
		})
	})
}

func TestCloser_IsClosed(t *testing.T) {
	t.Parallel()

	// One parent with a direct one-way child.
	p := New()
	c := OneWay(p)
	Hook(p, func(h H) {
		h.OnClose(func() {
			// Check here whether the child signals that it is completely closed.
			r.True(t, c.IsClosed())
			select {
			case <-c.ClosedChan():
			default:
				t.Fatal("child closer should be closed")
			}
		})
	})
}

func TestCloser_WaitPanic(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic")
		}
	}()

	c := toInternal(New())
	c.waitDone()
}

func TestCloser_Done(t *testing.T) {
	t.Parallel()

	c := toInternal(New())
	c.addWait(3)
	c.waitDone()
	c.waitDone()
	c.waitDone()
	go c.Close()

	select {
	case <-c.ClosedChan():
	case <-time.After(time.Second):
		t.Fatal("deadlock on close")
	}
}

func TestCloserErrors(t *testing.T) {
	t.Parallel()

	errSpecific := errors.New("specific error")

	c := New()
	Hook(c, func(h H) {
		for i := 0; i < 3; i++ {
			h.OnClosingWithErr(func() error { return errors.New("error closing") })
			h.OnCloseWithErr(func() error { return errors.New("error closed") })
		}
	})

	c1 := OneWay(c)
	Hook(c1, func(h H) {
		for i := 0; i < 3; i++ {
			h.OnClosingWithErr(func() error { return errors.New("error closing") })
			h.OnCloseWithErr(func() error { return errors.New("error closed") })
		}
		h.OnClosingWithErr(func() error { return errSpecific })
	})

	// Execute multiple times to test, that the error does not change between calls.
	for i := 0; i < 3; i++ {
		err := c.Close()
		r.Error(t, err)
		r.ErrorIs(t, err, errSpecific)
	}

	// Create new closer and test CloseWithErr and Error now.
	c = New()
	r.Nil(t, c.CloseErr())
	Hook(c, func(h H) {
		for i := 0; i < 3; i++ {
			h.OnClosingWithErr(func() error { return errors.New("error closing") })
			h.OnCloseWithErr(func() error { return errors.New("error closed") })
		}
	})

	// Execute multiple times to test, that the error does not change between calls.
	for i := 0; i < 3; i++ {
		c.AsyncCloseWithErr(errSpecific)
	}

	err := c.Close()
	r.Error(t, err)
	r.ErrorIs(t, err, errSpecific)
	r.Same(t, err, c.CloseErr())
}

func TestCloseRoutineError(t *testing.T) {
	t.Parallel()

	var (
		err = errors.New("error")
		c   = New()
	)

	Routine(c, func() error {
		<-c.ClosingChan()
		return err
	})

	time.Sleep(10 * time.Millisecond)

	r.ErrorIs(t, c.Close(), err)
}

func TestCloseFuncsLIFO(t *testing.T) {
	t.Parallel()

	orderChan := make(chan int, 4)

	c := New()
	Hook(c, func(h H) {
		h.OnClose(func() {
			orderChan <- 0
		})
		h.OnClose(func() {
			orderChan <- 1
		})
		h.OnClose(func() {
			orderChan <- 2
		})
		h.OnClose(func() {
			orderChan <- 3
		})
	})

	err := c.Close()
	r.NoError(t, err)

	for i := 3; i >= 0; i-- {
		r.Equal(t, i, <-orderChan)
	}
}

func TestCloser_Context(t *testing.T) {
	t.Parallel()

	// Test closing the
	c := New()
	ctx := Context(c)
	c.Close()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("deadlock on context")
	}
}

func TestCloser_ContextClose(t *testing.T) {
	t.Parallel()

	// Test closing the closer through the context.
	c := New()
	ctx, cancel := ContextWithCancel(c)
	OnContextDoneClose(ctx, c)
	time.Sleep(time.Millisecond)
	cancel()
	select {
	case <-c.ClosedChan():
	case <-time.After(time.Second):
		t.Fatal("timeout")
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
	p := New()
	c1 := OneWay(p)
	c2 := OneWay(c1)

	// Close a child and check that the parent does not close.
	err := c2.Close()
	r.NoError(t, err)
	r.False(t, c1.IsClosing())
	r.False(t, c1.IsClosed())

	Hook(p, func(h H) {
		h.OnClosing(func() {
			r.True(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.False(t, c1.IsClosing())
			r.False(t, c1.IsClosed())
		})

		h.OnClose(func() {
			r.True(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.True(t, c1.IsClosing())
			r.True(t, c1.IsClosed())
		})
	})

	// Close the parent now.
	err = p.Close()
	r.NoError(t, err)
	r.True(t, p.IsClosed())
}

func testOneWayRoutines(t *testing.T) {
	t.Parallel()

	// One parent with 2 direct, one-way children.
	p := New()
	c1 := OneWay(p)
	c2 := OneWay(p)

	Hook(p, func(h H) {
		h.OnClose(func() {
			// Both children must be closed before the parent closes.
			r.True(t, c1.IsClosed())
			r.True(t, c2.IsClosed())
		})
	})

	// The first child has a child of its own.
	cc1 := OneWay(c1)

	Hook(c1, func(h H) {
		h.OnClose(func() {
			// The child must be closed before.
			r.True(t, cc1.IsClosed())
		})
	})

	// Start routines for each of the children.
	retChan := make(chan error, 4)
	f := func(c Closer) {
		var err error
		select {
		case <-c.ClosedChan():
			err = c.CloseErr()
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
	p := New()
	c1 := TwoWay(p)
	c2 := TwoWay(c1)

	Hook(p, func(h H) {
		h.OnClose(func() {
			r.True(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.True(t, c1.IsClosing())
			r.True(t, c1.IsClosed())

			r.True(t, c2.IsClosing())
			r.True(t, c2.IsClosed())
		})
	})

	Hook(c1, func(h H) {
		h.OnClose(func() {
			// We closed the first-level child, at least the second-level child must be closed.
			// The parent must not be closing yet.
			r.False(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.True(t, c1.IsClosing())
			r.False(t, c1.IsClosed())

			r.True(t, c2.IsClosing())
			r.True(t, c2.IsClosed())
		})
	})

	Hook(c2, func(h H) {
		h.OnClose(func() {
			// We closed the second-level child, all others must not yet be closing.
			r.False(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.False(t, c1.IsClosing())
			r.False(t, c1.IsClosed())

			r.True(t, c2.IsClosing())
			r.False(t, c2.IsClosed())
		})
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
	p = New()
	c1 = TwoWay(p)
	c2 = TwoWay(c1)

	Hook(p, func(h H) {
		h.OnClose(func() {
			r.True(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.True(t, c1.IsClosing())
			r.True(t, c1.IsClosed())

			r.True(t, c2.IsClosing())
			r.True(t, c2.IsClosed())
		})
	})

	Hook(c1, func(h H) {
		h.OnClose(func() {
			// We close the child, so the parent and the other children must close first.
			r.True(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.True(t, c1.IsClosing())
			r.False(t, c1.IsClosed())

			r.True(t, c2.IsClosing())
			r.True(t, c2.IsClosed())
		})
	})

	Hook(c2, func(h H) {
		h.OnClose(func() {
			// We close the child, so the parent and the other children must close first.
			r.True(t, p.IsClosing())
			r.False(t, p.IsClosed())

			r.True(t, c1.IsClosing())
			r.False(t, c1.IsClosed())

			r.True(t, c2.IsClosing())
			r.False(t, c2.IsClosed())
		})
	})

	// Close the parent this time.
	err = p.Close()
	r.NoError(t, err)
	r.True(t, p.IsClosed())
}

func testTwoWayRoutines(t *testing.T) {
	t.Parallel()

	// One parent with 2 direct, two-way children.
	p := New()
	c1 := TwoWay(p)
	c2 := TwoWay(p)

	Hook(p, func(h H) {
		h.OnClose(func() {
			// Both children must be closed
			r.True(t, c1.IsClosed())
			r.True(t, c2.IsClosed())
		})
	})

	// This is a child of the first child. This closer
	// will be closed.
	cc1 := TwoWay(c1)
	Hook(cc1, func(h H) {
		h.OnClose(func() {
			r.False(t, p.IsClosed())
			r.False(t, c1.IsClosed())
			r.False(t, c2.IsClosed())
		})
	})

	Hook(c1, func(h H) {
		h.OnClose(func() {
			r.True(t, cc1.IsClosed())
			r.False(t, p.IsClosed())
			r.False(t, c2.IsClosed())
		})
	})

	retChan := make(chan error, 4)
	f := func(c Closer) {
		var err error
		select {
		case <-c.ClosedChan():
			err = c.CloseErr()
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

	p := New()
	c := TwoWay(p)

	// Will increment the wait group.
	Routine(p, func() error {
		c.Close()
		return nil
	})

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	case <-p.ClosedChan():
	}
}

func TestEndlessGrowth(t *testing.T) {
	t.Parallel()

	// Test is mainly there for 100% test coverage.

	c := New()
	children := make([]Closer, 0, 1000)
	for i := 0; i < 1000; i++ {
		children = append(children, OneWay(c))
	}
	for i := 0; i < 1000; i++ {
		r.NoError(t, children[i].Close())
	}
}

func TestCloser_BlockCloser(t *testing.T) {
	t.Parallel()

	var (
		v  atomic.Bool
		ch = make(chan struct{})
		c  = New()
	)

	go func() {
		_ = Block(c, func() (err error) {
			<-ch
			ch <- struct{}{}
			if c.IsClosed() {
				v.Store(true)
			}
			ch <- struct{}{}
			return nil
		})
	}()

	ch <- struct{}{}
	go c.Close()
	time.Sleep(time.Second)
	<-ch
	<-ch

	r.False(t, v.Load())
}

func TestCloser_BlockCloser_DoNotRunIfClosed(t *testing.T) {
	t.Parallel()

	c := New()
	c.Close()

	var v atomic.Bool
	err := Block(c, func() (err error) {
		v.Store(true)
		return nil
	})

	r.False(t, v.Load())
	r.ErrorIs(t, err, ErrClosed)
}

func TestCloser_RunCloserRoutine(t *testing.T) {
	t.Parallel()

	var (
		c       = New()
		started = make(chan struct{})
		err     = errors.New("error")
	)

	Routine(c, func() error {
		close(started)
		return err
	})

	select {
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	case <-started:
	}

	c.Close()
	r.ErrorIs(t, c.CloseErr(), err)
}

func TestCloser_RunCloserRoutine_DoNotRunIfClosed(t *testing.T) {
	t.Parallel()

	c := New()
	c.Close()

	var v atomic.Bool
	Routine(c, func() (err error) {
		v.Store(true)
		return nil
	})

	time.Sleep(time.Second)
	r.False(t, v.Load())
}

func TestCloser_NewCloser_DoNotRunIfClosed(t *testing.T) {
	t.Parallel()

	c := New()
	c.Close()

	cc := TwoWay(c)
	r.True(t, cc.IsClosing())
	r.True(t, cc.IsClosed())

	cc = OneWay(c)
	r.True(t, cc.IsClosing())
	r.True(t, cc.IsClosed())
}
