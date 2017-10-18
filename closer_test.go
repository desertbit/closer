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

package closer

import (
	"fmt"
	"sync"
	"testing"
)

func TestCloser(t *testing.T) {
	c := New()
	if c.IsClosed() {
		t.Error()
	}
	select {
	case <-c.CloseChan:
		t.Error()
	default:
	}

	err := c.Close()
	if !c.IsClosed() {
		t.Error()
	} else if err != nil {
		t.Error()
	}
	select {
	case <-c.CloseChan:
	default:
		t.Error()
	}

	err = c.Close()
	if !c.IsClosed() {
		t.Error()
	} else if err != nil {
		t.Error()
	}
}

func TestCloserWithInterface(t *testing.T) {
	c := New(func() error {
		return fmt.Errorf("error")
	})
	if c.IsClosed() {
		t.Error()
	}

	err := c.Close()
	if err == nil || err.Error() != "error" {
		t.Error()
	}

	err = c.Close()
	if err != nil {
		t.Error()
	}
}

func TestConcurrentCloser(t *testing.T) {
	c := New()
	wg := sync.WaitGroup{}

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			c.Close()
			wg.Done()
		}()
	}

	wg.Wait()

	if !c.IsClosed() {
		t.Error()
	}

	select {
	case <-c.CloseChan:
	default:
		t.Error()
	}
}
