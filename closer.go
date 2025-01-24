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

// Package closer offers a simple, thread-safe closer.
//
// It allows to build up a tree of closing relationships, where you typically
// start with a root closer that branches into different children and
// children's children. When a parent closer spawns a child closer, the child
// either has a one-way or two-way connection to its parent. One-way children
// are closed when their parent closes. In addition, two-way children also close
// their parent, if they are closed themselves.
//
// A closer is also useful to ensure that certain dependencies, such as network
// connections, are reliably taken down, once the closer closes.
// In addition, a closer can be concurrently closed many times, without closing
// more than once, but still returning the errors to every caller.
//
// This allows to represent complex closing relationships and helps avoiding
// leaking goroutines, gracefully shutting down, etc.
package closer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// ErrClosed is a generic error that indicates a resource has been closed.
var ErrClosed = errors.New("closed")

// TODO: doc.
type Closer interface {
	Close() error
	CloseErr() error

	AsyncClose()
	AsyncCloseWithErr(error)

	ClosingChan() <-chan struct{}
	ClosedChan() <-chan struct{}

	IsClosing() bool
	IsClosed() bool
}

// New creates a new closer without any parent relation.
func New() Closer {
	return newCloser(3)
}

func TwoWay(p Closer) Closer {
	return toInternal(p).addChild(true)
}

func OneWay(p Closer) Closer {
	return toInternal(p).addChild(false)
}

// TODO: doc.
func Block(cl Closer, f func() error) error {
	var trace string
	if debugEnabled {
		trace = stacktrace(2)
	}

	c := toInternal(cl)
	c.addWait(1)

	return func() error {
		defer c.waitDone()

		// addWait also adds to a closed closer. Ensure we are not in a closing state.
		// From now on the closer will wait for the function to exit before closing.
		if c.IsClosing() {
			return ErrClosed
		}

		// Print a debug stacktrace if build with debugging mode.
		if debugEnabled {
			doneChan := make(chan struct{})
			go func() {
				<-c.closingChan

				t := time.NewTimer(debugLogAfterTimeout)
				defer t.Stop()

				select {
				case <-doneChan:
					return
				case <-t.C:
					// Use fmt instead of log for additional new line printing.
					fmt.Fprintf(os.Stderr, "\nDEBUG: closer.Block takes longer than expected to close:\n%s\n\n", trace)
				}
			}()
			defer close(doneChan)
		}

		return f()
	}()
}

func Wait(cl Closer, ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-cl.ClosedChan():
		return cl.CloseErr()
	}
}

func WaitChan(cl Closer, ctx context.Context) <-chan error {
	waitChan := make(chan error, 1)
	go func() {
		waitChan <- Wait(cl, ctx)
	}()
	return waitChan
}

func Routine(cl Closer, f func() error) {
	var trace string
	if debugEnabled {
		trace = stacktrace(2)
	}

	c := toInternal(cl)
	c.addWait(1)

	go func() {
		defer func() {
			c.waitDone() // Must be before Close().
			c.Close()    // This might block.
		}()

		// addWait also adds to a closed closer. Ensure we are not in a closing state.
		// From now on the closer will wait for the routine to exit before closing.
		if c.IsClosing() {
			return
		}

		// Print a debug stacktrace if build with debugging mode.
		if debugEnabled {
			doneChan := make(chan struct{})
			go func() {
				<-c.closingChan

				t := time.NewTimer(debugLogAfterTimeout)
				defer t.Stop()

				select {
				case <-doneChan:
					return
				case <-t.C:
					// Use fmt instead of log for additional new line printing.
					fmt.Fprintf(os.Stderr, "\nDEBUG: closer.Routine takes longer than expected to close:\n%s\n\n", trace)
				}
			}()
			defer close(doneChan)
		}

		err := f()
		if err != nil {
			c.addError(err)
		}
	}()
}

func RoutineWithCloser(cl Closer, f func(Closer) error) {
	Routine(cl, func() error {
		return f(cl)
	})
}

//######################//
//### Implementation ###//
//######################//

// The closer type is this package's implementation of the Closer interface.
type closer struct {
	// An unbuffered channel that expresses whether the
	// closer is about to close.
	// The channel itself gets closed to represent the closing
	// of the closer, which leads to reads off of it to succeed.
	closingChan chan struct{}
	// An unbuffered channel that expresses whether the
	// closer has been completely closed.
	// The channel itself gets closed to represent the closing
	// of the closer, which leads to reads off of it to succeed.
	closedChan chan struct{}
	// The error collected by executing the Close() func
	// and combining all encountered errors from the close funcs as joined error.
	closeErr error

	// Synchronises the access to the following properties.
	mx sync.Mutex
	// The close funcs that are executed when this closer closes.
	closeFuncs []func() error
	// The closing funcs that are executed when this closer closes.
	closingFuncs []func() error
	// The parent of this closer. May be nil.
	parent *closer
	// The closer children that this closer spawned.
	children []*closer
	// Used to wait for external dependencies of the closer
	// before the Close() method actually returns.
	// Use a custom implementation, because the sync.WaitGroup Wait() method is not thread-safe.
	waitCond  *sync.Cond
	waitCount int64

	// A flag that indicates whether this closer is a two-way closer.
	// In comparison to a standard one-way closer, which closes when
	// its parent closes, a two-way closer closes also its parent, when
	// it itself gets closed.
	twoWay bool

	// The index of this closer in its parent's children slice.
	// Needed to efficiently remove the closer from its parent.
	parentIndex int
}

func newCloser(debugSkipStacktrace int) *closer {
	c := &closer{
		closingChan: make(chan struct{}),
		closedChan:  make(chan struct{}),
	}
	c.waitCond = sync.NewCond(&c.mx)

	// Print a debug stacktrace if build with debugging mode.
	if debugEnabled {
		trace := stacktrace(debugSkipStacktrace)
		go func() {
			<-c.closingChan

			t := time.NewTimer(debugLogAfterTimeout)
			defer t.Stop()

			select {
			case <-c.closedChan:
				return
			case <-t.C:
				// Use fmt instead of log for additional new line printing.
				fmt.Fprintf(os.Stderr, "\nDEBUG: Closer takes longer than expected to close:\n%s\n\n", trace)
			}
		}()
	}

	return c
}

// Implements the Closer interface.
func (c *closer) Close() error {
	// Close the closing channel to signal that this closer is about to close now.
	// Do this in a locked context and release as soon as the channel is closed.
	// If another close call is handling this context, then wait for it to exit before returning the error.
	c.mx.Lock()
	if c.IsClosing() {
		c.mx.Unlock()
		<-c.closedChan
		return c.closeErr
	}
	close(c.closingChan)
	// Copy the internal variables to local variables. Otherwise direct access could cause a race.
	var (
		closingFuncs = c.closingFuncs
		closeFuncs   = c.closeFuncs
		children     = c.children
	)
	c.closingFuncs = nil
	c.closeFuncs = nil
	c.children = nil
	c.mx.Unlock()

	// We are in an unlocked state. Do not use c.closeErr directly.
	var closeErrors error

	// Execute all closing funcs of this closer in LIFO order.
	for i := len(closingFuncs) - 1; i >= 0; i-- {
		closeErrors = errors.Join(closeErrors, closingFuncs[i]())
	}

	// Close all children and join their errors.
	for _, child := range children {
		closeErrors = errors.Join(closeErrors, child.Close())
	}

	// Wait, until all dependencies of this closer have closed.
	c.mx.Lock()
	for c.waitCount > 0 {
		c.waitCond.Wait()
	}
	c.mx.Unlock()

	// Execute all close funcs of this closer in LIFO order.
	for i := len(closeFuncs) - 1; i >= 0; i-- {
		closeErrors = errors.Join(closeErrors, closeFuncs[i]())
	}

	// Close the closed channel to signal that this closer is closed now.
	// Finally merge the errors. Do this in a locked context.
	c.mx.Lock()
	c.closeErr = errors.Join(c.closeErr, closeErrors)
	close(c.closedChan)
	c.mx.Unlock()

	// Close the parent now as well, if this is a two way closer.
	// Otherwise, the closer must remove its reference from its parent's children
	// to prevent a leak.
	// Only perform these actions, if the parent is not closing already!
	if c.parent != nil && !c.parent.IsClosing() {
		if c.twoWay {
			// Do not wait for the parent close. This may cause a dead-lock.
			// Traversing up the closer tree does not require that the children wait for their parents.
			c.parent.AsyncClose()
		} else {
			c.parent.removeChild(c)
		}
	}

	return c.closeErr
}

// Implements the Closer interface.
func (c *closer) CloseErr() (err error) {
	if c.IsClosed() {
		// No need for mutex lock since the closeErr is not modified
		// after the closer has closed.
		err = c.closeErr
	}
	return
}

// Implements the Closer interface.
func (c *closer) AsyncClose() {
	if !c.IsClosing() {
		go c.Close()
	}
}

// Implements the Closer interface.
func (c *closer) AsyncCloseWithErr(err error) {
	c.addError(err)
	c.AsyncClose()
}

// Implements the Closer interface.
func (c *closer) ClosingChan() <-chan struct{} {
	return c.closingChan
}

// Implements the Closer interface.
func (c *closer) ClosedChan() <-chan struct{} {
	return c.closedChan
}

// Implements the Closer interface.
func (c *closer) IsClosing() bool {
	select {
	case <-c.closingChan:
		return true
	default:
		return false
	}
}

// Implements the Closer interface.
func (c *closer) IsClosed() bool {
	select {
	case <-c.closedChan:
		return true
	default:
		return false
	}
}

func (c *closer) addError(err error) {
	c.mx.Lock()
	defer c.mx.Unlock()

	// If the closer is already closed, then don't add errors.
	if c.IsClosed() {
		return
	}

	// Join the error.
	c.closeErr = errors.Join(c.closeErr, err)
}

// addChild creates a new closer and adds it as either
// a one-way or two-way child to this closer.
func (c *closer) addChild(twoWay bool) *closer {
	// Create a new closer and set the current closer as its parent.
	// Also set the twoWay flag.
	child := newCloser(4)
	child.parent = c
	child.twoWay = twoWay

	c.mx.Lock()
	defer c.mx.Unlock()

	// Close the new closer if the parent is already closed.
	if c.IsClosing() {
		child.Close()
		return child
	}

	// Add the closer to the current closer's children.
	child.parentIndex = len(c.children)
	c.children = append(c.children, child)

	return child
}

// removeChild removes the given child from this closer's children.
// If the child can not be found, this is a no-op.
func (c *closer) removeChild(child *closer) {
	const (
		minChildrenCap = 100
	)

	c.mx.Lock()
	defer c.mx.Unlock()

	last := len(c.children) - 1
	if last < 0 {
		return
	}

	c.children[last].parentIndex = child.parentIndex
	c.children[child.parentIndex] = c.children[last]
	c.children[last] = nil
	c.children = c.children[:last]

	// Prevent endless growth.
	// If the capacity is bigger than our min value and
	// four times larger than the length, shrink it by half.
	cp := cap(c.children)
	le := len(c.children)
	if cp > minChildrenCap && cp > 4*le {
		children := make([]*closer, le, le*2)
		copy(children, c.children)
		c.children = children
	}
}

func (c *closer) addWait(delta int) {
	c.mx.Lock()
	c.waitCount += int64(delta)
	c.mx.Unlock()
}

func (c *closer) waitDone() {
	c.mx.Lock()
	defer c.mx.Unlock()

	c.waitCount--
	c.waitCond.Broadcast()

	if c.waitCount < 0 {
		panic("CloserDone: negative wait counter")
	}
}

func toInternal(cl Closer) *closer {
	return cl.(*closer)
}
