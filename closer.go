/*
 *  Closer - A simple thread-safe closer
 *  Copyright (C) 2019  Roland Singer <roland.singer[at]desertbit.com>
 *  Copyright (C) 2019  Sebastian Borchers <sebastian[at]desertbit.com>
 *
 *  This program is free software: you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation, either version 3 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

// Package closer offers a simple thread-safe closer.
//
// It allows to build up a tree of closing relationships, where you typically
// start with a root closer that branches into different children and
// children's children. When a parent closer spawns a child closer, the child
// either has a one-way or two-way connection to its parent. One-way children
// are closed when their parent closes. In addition, two-way children also close
// their parent, if they are closed themselves.
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

	// CloseChan returns a channel, which is closed as
	// soon as the closer is closed.
	CloseChan() <-chan struct{}

	// IsClosed returns a boolean indicating
	// whether this instance has been closed.
	IsClosed() bool

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
	// closer has been closed already.
	// The channel itself gets closed to represent the closing
	// of the closer, which leads to reads off of it to succeed.
	closeChan chan struct{}
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
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// If the closer is already closed, just return the error.
	if c.IsClosed() {
		return c.closeErr
	}

	// Close the channel to signal that this closer is closed now.
	close(c.closeChan)

	// Close all children.
	for _, child := range c.children {
		// Do not attempt to close a child here that is already closed.
		// If the child is already contained in the closing chain, this
		// leads to a deadlock on the closer's mutex, since a upper Close()
		// call of this same child might then be waiting for its Close()
		// method and its own mutex again.
		if !child.IsClosed() {
			_ = child.Close()
		}
	}

	// Wait, until all dependencies of this closer have closed.
	c.wg.Wait()

	// If this is a twoWay closer, close the parent now as well,
	// but only if it has not been closed yet! Otherwise there is a
	// deadlock on the mutex, when the closing chain already closed the parent
	// and it is waiting on its own Close() method to return.
	if c.twoWay && c.parent != nil && !c.parent.IsClosed() {
		_ = c.parent.Close()
	}

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

	return c.closeErr
}

// Implements the Closer interface.
func (c *closer) CloseAndDone() error {
	c.wg.Done()
	return c.Close()
}

// Implements the Closer interface.
func (c *closer) CloseChan() <-chan struct{} {
	return c.closeChan
}

// Implements the Closer interface.
func (c *closer) IsClosed() bool {
	select {
	case <-c.closeChan:
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
		closeChan: make(chan struct{}),
		funcs:     f,
		children:  make([]*closer, 0),
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
