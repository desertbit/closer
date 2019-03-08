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

package closer_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/desertbit/closer"
	"github.com/hashicorp/go-multierror"
	"github.com/stretchr/testify/require"
)

func TestCloser_Close(t *testing.T) {
	t.Parallel()

	// Test the closer with nil errors.
	c := closer.New()
	require.False(t, c.IsClosed())
	select {
	case <-c.CloseChan():
		t.Fatal("close chan should not read error")
	default:
	}

	// Nil error expected. Perform this two times, as we
	// expect the Close method to behave always the same,
	// regardless of how many times we call it.
	for i := 0; i < 2; i++ {
		err := c.Close()
		require.True(t, c.IsClosed())
		require.NoError(t, err)
		select {
		case <-c.CloseChan():
		default:
			t.Fatal("close chan should read error")
		}
	}

	// Test closer with non-nil errors.
	c = closer.New(func() error {
		return errors.New("test")
	})

	require.False(t, c.IsClosed())
	select {
	case <-c.CloseChan():
		t.Fatal("close chan should not read error")
	default:
	}

	// Error expected. Perform this two times, as we
	// expect the Close method to behave always the same,
	// regardless of how many times we call it.
	for i := 0; i < 2; i++ {
		err := c.Close()
		require.True(t, c.IsClosed())
		require.Error(t, err)
		select {
		case <-c.CloseChan():
		default:
			t.Fatal("close chan should read error")
		}
	}
}

func TestCloserError(t *testing.T) {
	t.Parallel()

	c := closer.New(func() error {
		return errors.New("error")
	})

	err := c.Close()
	require.IsType(t, &multierror.Error{}, err)
	require.Error(t, err)
	require.Len(t, err.(*multierror.Error).Errors, 1)
	require.Equal(t, "error", err.(*multierror.Error).Errors[0].Error())
}

func TestCloserWithFunc(t *testing.T) {
	t.Parallel()

	c := closer.New(func() error {
		return errors.New("error")
	})
	require.False(t, c.IsClosed())

	for i := 0; i < 3; i++ {
		err := c.Close()
		require.IsType(t, &multierror.Error{}, err)
		require.Error(t, err)
		require.Len(t, err.(*multierror.Error).Errors, 1)
		require.Equal(t, "error", err.(*multierror.Error).Errors[0].Error())
	}
}

func TestCloserWithFuncs(t *testing.T) {
	t.Parallel()

	c := closer.New(func() error {
		return errors.New("error")
	})
	require.False(t, c.IsClosed())

	for i := 0; i < 3; i++ {
		c.OnClose(func() error {
			return errors.New("error")
		})
	}

	for i := 0; i < 3; i++ {
		err := c.Close()
		require.Error(t, err)
		require.IsType(t, &multierror.Error{}, err)
		require.Len(t, err.(*multierror.Error).Errors, 4)

		for i := 0; i < 4; i++ {
			require.Equal(t, "error", err.(*multierror.Error).Errors[i].Error())
		}
	}
}

func TestCloseFuncsLIFO(t *testing.T) {
	t.Parallel()

	orderChan := make(chan int, 4)

	c := closer.New(func() error {
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
	require.NoError(t, err)

	for i := 3; i >= 0; i-- {
		require.Equal(t, i, <-orderChan)
	}
}

func TestCloser_OneWay(t *testing.T) {
	// Simple test case.
	t.Run("CloserOneWay - CloseFunc", testOneWayCloseFunc)

	// Complex test case.
	t.Run("CloserOneWay - Routines", testOneWayRoutines)
}

func testOneWayCloseFunc(t *testing.T) {
	t.Parallel()

	test := 0

	// One parent with 3 direct, one-way children.
	p := closer.New(func() error {
		// If the value is not 3 that means the child closers
		// were not close before the parent.
		require.Equal(t, 3, test, "a child closed after its parent")
		return nil
	})
	f := func() error {
		test++
		return nil
	}
	_ = p.CloserOneWay(f)
	_ = p.CloserOneWay(f)
	_ = p.CloserOneWay(f)

	// Close the parent now.
	_ = p.Close()
}

func testOneWayRoutines(t *testing.T) {
	t.Parallel()

	mx := sync.Mutex{}
	test := 0
	var ccClosed bool

	// One parent with 2 direct, one-way children.
	p := closer.New(func() error {
		require.Equal(t, 2, test, "a child closed after its parent")
		return nil
	})
	// This child has a child as well.
	c1 := p.CloserOneWay(func() error {
		require.True(t, ccClosed, "a child of a child closed after its parent")
		return nil
	})
	c2 := p.CloserOneWay()

	cc1 := c1.CloserOneWay()

	c1.CloserAddWaitGroup(1)
	c2.CloserAddWaitGroup(1)
	cc1.CloserAddWaitGroup(1)

	// Start a routine for each of the children.
	f := func(c closer.Closer) {
		defer func() {
			_ = c.CloseAndDone()
		}()

		select {
		case <-c.CloseChan():
			mx.Lock()
			test++
			mx.Unlock()
		case <-time.After(time.Second):
			t.Fatal("routine timed out")
		}
	}
	go f(c1)
	go f(c2)
	go func() {
		defer func() {
			_ = cc1.CloseAndDone()
		}()

		select {
		case <-cc1.CloseChan():
			ccClosed = true
		case <-time.After(time.Second):
			t.Fatal("routine timed out")
		}
	}()

	// Close the parent. This should close c1 and c2, before p closes.
	// c1 should close cc1 before it closes itself.
	_ = p.Close()
}

func TestCloser_TwoWay(t *testing.T) {
	// Simple test case.
	t.Run("CloserTwoWay - CloseFunc", testTwoWayCloseFunc)

	// Complex test case.
	t.Run("CloserTwoWay - Routines", testTwoWayRoutines)
}

func testTwoWayCloseFunc(t *testing.T) {
	t.Parallel()

	test := 0
	var parentClosed bool

	// One parent with 3 direct, two-way children.
	p := closer.New(func() error {
		// If the value is not 2 that means the child closers
		// were not closed before the parent.
		require.Equal(t, 2, test, "a child closed after its parent")
		parentClosed = true
		return nil
	})
	f := func() error {
		test++
		return nil
	}
	_ = p.CloserTwoWay(f)
	_ = p.CloserTwoWay(f)
	c := p.CloserTwoWay(f)

	// Close the child now, which should also close the parent.
	_ = c.Close()
	require.True(t, parentClosed)
}

func testTwoWayRoutines(t *testing.T) {
	t.Parallel()

	var pClosed, c1Closed, c2Closed bool

	// One parent with 2 direct, two-way children.
	p := closer.New(func() error {
		// c2 is only closed after its parent p.
		require.True(t, c2Closed)
		return nil
	})
	// This child has a child as well.
	c1 := p.CloserTwoWay()
	c2 := p.CloserTwoWay()

	cc1 := c1.CloserTwoWay(func() error {
		require.True(t, pClosed)
		require.True(t, c1Closed)
		require.True(t, c2Closed)
		return nil
	})

	p.CloserAddWaitGroup(1)
	c1.CloserAddWaitGroup(1)
	c2.CloserAddWaitGroup(1)

	// Start a routine for each of the children.
	f := func(c closer.Closer, b *bool) {
		select {
		case <-c.CloseChan():
			*b = true
			_ = c.CloseAndDone()
		case <-time.After(time.Second):
			t.Fatal("routine timed out")
		}
	}
	go f(c1, &c1Closed)
	go f(c2, &c2Closed)
	go f(p, &pClosed)

	// Close the lowest child. This should close c1, c2 and p.
	_ = cc1.Close()
}
