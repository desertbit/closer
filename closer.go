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
	// Close in a thread-safe manner. implements the io.Closer interface.
	// This method returns always the close error, regardless of how often
	// this method is called. Close blocks until all close functions are done,
	// no matter which goroutine called this method.
	// Returns a hashicorp multierror.
	Close() error

	// Attention: Calling this without first adding to the WaitGroup by
	// calling CloserAddWaitGroup() results in a panic.
	CloseAndDone() error

	// CloseChan channel which is closed as
	// soon as the closer is closed.
	CloseChan() <-chan struct{}

	// TODO
	CloserAddWaitGroup(delta int)

	// TODO
	CloserOneWay(f ...CloseFunc) Closer

	// TODO
	CloserTwoWay(f ...CloseFunc) Closer

	// IsClosed returns a boolean indicating
	// if this instance was closed.
	IsClosed() bool

	// Calls the close function on close.
	// Errors are appended to the Close() multi error.
	// Close functions are called in LIFO order.
	OnClose(f ...CloseFunc)
}

//######################//
//### Implementation ###//
//######################//

// The closer type TODO
type closer struct {
	// TODO
	closeChan chan struct{}
	closeErr  error

	mutex    sync.Mutex
	funcs    []CloseFunc
	parent   *closer
	children []*closer
	wg       sync.WaitGroup

	twoWay bool
}

// New creates a new closer.
// Optional pass functions which are called only once during close.
// Close function are called in LIFO order.
func New(f ...CloseFunc) Closer {
	return newCloser(f...)
}

// Implements the Closer interface.
func (c *closer) CloseChan() <-chan struct{} {
	return c.closeChan
}

// Implements the Closer interface.
func (c *closer) CloseAndDone() error {
	c.wg.Done()
	return c.Close()
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
func (c *closer) CloserAddWaitGroup(delta int) {
	c.mutex.Lock()
	c.wg.Add(delta)
	c.mutex.Unlock()
}

// Implements the Closer interface.
func (c *closer) CloserOneWay(f ...CloseFunc) Closer {
	return c.addChild(false, f...)
}

// Implements the Closer interface.
func (c *closer) CloserTwoWay(f ...CloseFunc) Closer {
	return c.addChild(true, f...)
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

//###############//
//### Private ###//
//###############//

// TODO
func newCloser(f ...CloseFunc) *closer {
	return &closer{
		closeChan: make(chan struct{}),
		funcs:     f,
		children:  make([]*closer, 0),
	}
}

// TODO
func (c *closer) addChild(bidirectional bool, f ...CloseFunc) *closer {
	// Create a new closer and set the current closer as its parent.
	// Also set the twoWay flag.
	child := newCloser(f...)
	child.parent = c
	child.twoWay = bidirectional

	// Add the new closer to the current closer's children.
	c.mutex.Lock()
	c.children = append(c.children, child)
	c.mutex.Unlock()

	return child
}
