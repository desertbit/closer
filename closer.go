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
	"fmt"
	"sync"

	multierror "github.com/hashicorp/go-multierror"
)

//#############//
//### Types ###//
//#############//

// CloseFunc defines the general close function.
type CloseFunc func() error

//#################//
//### Interface ###//
//#################//

// A Closer is a thread-safe helper for common close actions.
type Closer interface {
	// AddWaitGroup adds the given delta to the closer's
	// wait group. Useful to wait for routines associated
	// with this closer to gracefully shutdown.
	AddWaitGroup(delta int)

	// Close closes this closer in a thread-safe manner.
	//
	// Implements the io.Closer interface.
	//
	// This method returns always the close error, regardless of how often
	// it gets called. Close blocks, until all close functions are done,
	// no matter which goroutine called this method.
	// Returns a hashicorp multierror.
	Close() error

	// CloseAndDone performs the same operation as Close(), but decrements
	// the closer's wait group by one beforehand.
	// Attention: Calling this without first adding to the WaitGroup by
	// calling AddWaitGroup() results in a panic.
	CloseAndDone() error

	// ClosingChan returns a channel, which is closed as
	// soon as the closer is about to close.
	// Remains closed, once ClosedChan() has also been closed.
	ClosingChan() <-chan struct{}

	// ClosedChan returns a channel, which is closed as
	// soon as the closer is completely closed.
	ClosedChan() <-chan struct{}

	// Done decrements the closer's wait group by one.
	// Attention: Calling this without first adding to the WaitGroup by
	// calling AddWaitGroup() results in a panic.
	Done()

	// IsClosed returns a boolean indicating
	// whether this instance has been closed completely.
	IsClosed() bool

	// IsClosing returns a boolean indicating
	// whether this instance is about to close.
	// Also returns true, if IsClosed() returns true.
	IsClosing() bool

	// Calls the close function on close.
	// Errors are appended to the Close() multi error.
	// Close functions are called in LIFO order.
	OnClose(f ...CloseFunc)

	// OneWay creates a new child closer that has a one-way relationship
	// with the current closer. This means that the child is closed whenever
	// the parent closes, but not vice versa.
	OneWay(f ...CloseFunc) Closer

	// TwoWay creates a new child closer that has a two-way relationship
	// with the current closer. This means that the child is closed whenever
	// the parent closes and vice versa.
	TwoWay(f ...CloseFunc) Closer
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
	// and combining all encountered errors from the close funcs.
	closeErr error

	// Synchronises the access to the following properties.
	mutex sync.Mutex
	// The close funcs that are executed when this closer closes.
	funcs []CloseFunc
	// The parent of this closer. May be nil.
	parent *closer
	// The closer children that this closer spawned.
	children []*closer
	// Used to wait for external dependencies of the closer
	// before the Close() method actually returns.
	wg sync.WaitGroup

	// A flag that indicates whether this closer is a two-way closer.
	// In comparison to a standard one-way closer, which closes when
	// its parent closes, a two-way closer closes also its parent, when
	// it itself gets closed.
	twoWay bool
}

// New creates a new closer.
// Optional pass functions which are called only once during close.
// Close function are called in LIFO order.
func New(f ...CloseFunc) Closer {
	return newCloser(f...)
}

// Implements the Closer interface.
func (c *closer) AddWaitGroup(delta int) {
	c.wg.Add(delta)
}

// Implements the Closer interface.
func (c *closer) Close() error {
	// Mutex is not unlocked on defer! Therefore, be cautious when adding
	// new control flow statements like return.
	c.mutex.Lock()

	// If the closer is already closing, just return the error.
	if c.IsClosing() {
		c.mutex.Unlock()
		return c.closeErr
	}

	// Close the closing channel to signal that this closer is about to close now.
	close(c.closingChan)

	// Close all children.
	for _, child := range c.children {
		_ = child.Close()
	}

	// Wait, until all dependencies of this closer have closed.
	c.wg.Wait()

	// Execute all close funcs of this closer.
	// Batch errors together.
	var mErr *multierror.Error

	// Call in LIFO order. Append the errors.
	for i := len(c.funcs) - 1; i >= 0; i-- {
		if err := c.funcs[i](); err != nil {
			mErr = multierror.Append(mErr, err)
		}
	}
	c.funcs = nil

	if mErr != nil {
		// The default multiCloser error formatting uses too much space.
		mErr.ErrorFormat = func(errors []error) string {
			str := fmt.Sprintf("%v close errors occurred:", len(errors))
			for _, err := range errors {
				str += "\n- " + err.Error()
			}
			return str
		}
		c.closeErr = mErr
	}

	// Close the closed channel to signal that this closer is closed now.
	close(c.closedChan)

	c.mutex.Unlock()

	// If this is a twoWay closer, close the parent now as well,
	// but only if it not closing already!
	if c.twoWay && c.parent != nil && !c.parent.IsClosing() {
		_ = c.parent.Close()
	}

	return c.closeErr
}

// Implements the Closer interface.
func (c *closer) CloseAndDone() error {
	c.wg.Done()
	return c.Close()
}

// Implements the Closer interface.
func (c *closer) ClosedChan() <-chan struct{} {
	return c.closedChan
}

// Implements the Closer interface.
func (c *closer) ClosingChan() <-chan struct{} {
	return c.closingChan
}

// Implements the Closer interface.
func (c *closer) Done() {
	c.wg.Done()
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
func (c *closer) OnClose(f ...CloseFunc) {
	c.mutex.Lock()
	c.funcs = append(c.funcs, f...)
	c.mutex.Unlock()
}

// Implements the Closer interface.
func (c *closer) OneWay(f ...CloseFunc) Closer {
	return c.addChild(false, f...)
}

// Implements the Closer interface.
func (c *closer) TwoWay(f ...CloseFunc) Closer {
	return c.addChild(true, f...)
}

//###############//
//### Private ###//
//###############//

// newCloser creates a new closer with the given close funcs.
func newCloser(f ...CloseFunc) *closer {
	return &closer{
		closingChan: make(chan struct{}),
		closedChan:  make(chan struct{}),
		funcs:       f,
		children:    make([]*closer, 0),
	}
}

// addChild creates a new closer with the given close funcs, and
// adds it as either a one-way or two-way child to this closer.
func (c *closer) addChild(twoWay bool, f ...CloseFunc) *closer {
	// Create a new closer and set the current closer as its parent.
	// Also set the twoWay flag.
	child := newCloser(f...)
	child.parent = c
	child.twoWay = twoWay

	// Add the new closer to the current closer's children.
	c.mutex.Lock()
	c.children = append(c.children, child)
	c.mutex.Unlock()

	return child
}
