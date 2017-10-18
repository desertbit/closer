/*
 *  Closer - A simple thread-safe closer
 *  Copyright (C) 2016  Roland Singer <roland.singer[at]desertbit.com>
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

import "sync"

//##############//
//### Closer ###//
//##############//

// Closer defines a thread-safe closer.
type Closer struct {
	// CloseChan channel which is closed as soon as the closer is closed.
	CloseChan <-chan struct{}

	mutex     sync.Mutex
	closeChan chan struct{}
	f         func() error
}

// New creates a new closer.
// Optional pass a function which is called only once during close.
func New(f ...func() error) *Closer {
	var ff func() error
	if len(f) > 0 {
		ff = f[0]
	}

	closeChan := make(chan struct{})
	return &Closer{
		CloseChan: closeChan,
		closeChan: closeChan,
		f:         ff,
	}
}

// IsClosed returns a boolean indicating if this instance was closed.
func (c *Closer) IsClosed() bool {
	select {
	case <-c.closeChan:
		return true
	default:
		return false
	}
}

// Close in a thread-safe manner.
// implements the io.Closer interface.
func (c *Closer) Close() error {
	// Close the close channel if not already closed.
	c.mutex.Lock()
	if c.IsClosed() {
		c.mutex.Unlock()
		return nil
	}
	close(c.closeChan)
	c.mutex.Unlock()

	if c.f == nil {
		return nil
	}

	return c.f()
}
