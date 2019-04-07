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
	"testing"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"github.com/desertbit/closer"

	"github.com/stretchr/testify/require"
)

func TestCloser_Close(t *testing.T) {
	t.Parallel()

	// Test the closer with nil errors.
	c := closer.New()
	require.False(t, c.IsClosed())
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
		require.True(t, c.IsClosed())
		require.NoError(t, err)
		select {
		case <-c.ClosedChan():
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
	case <-c.ClosedChan():
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
		require.True(t, p.IsClosing())
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
	c := p.OneWay()
	p.OnClose(func() error {
		// Check here whether the child signals that it is completely closed.
		require.True(t, c.IsClosed())
		select {
		case <-c.ClosedChan():
		default:
			t.Fatal("child closer should be closed")
		}
		return nil
	})
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
	t.Run("OneWay - CloseFunc", testOneWayCloseFunc)

	// Complex test case.
	t.Run("OneWay - Routines", testOneWayRoutines)
}

func testOneWayCloseFunc(t *testing.T) {
	t.Parallel()

	// A closer chain with only one-way closers.
	p := closer.New()
	c1 := p.OneWay()
	c2 := c1.OneWay()

	// Close a child and check that the parent does not close.
	err := c2.Close()
	require.NoError(t, err)
	require.False(t, c1.IsClosing())
	require.False(t, c1.IsClosed())

	// Close the parent now and check that the child closes.
	p.OnClose(func() error {
		require.True(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.True(t, c1.IsClosing())
		require.True(t, c1.IsClosed())
		return nil
	})
	err = p.Close()
	require.NoError(t, err)
	require.True(t, p.IsClosed())
}

func testOneWayRoutines(t *testing.T) {
	t.Parallel()

	// One parent with 2 direct, one-way children.
	p := closer.New()
	c1 := p.OneWay()
	c2 := p.OneWay()

	p.OnClose(func() error {
		// Both children must be closed before the parent closes.
		require.True(t, c1.IsClosed())
		require.True(t, c2.IsClosed())
		return nil
	})

	// The first child has a child of its own.
	cc1 := c1.OneWay()

	c1.OnClose(func() error {
		// The child must be closed before.
		require.True(t, cc1.IsClosed())
		return nil
	})

	c1.AddWaitGroup(1)
	c2.AddWaitGroup(1)
	cc1.AddWaitGroup(2) // Try two routines.

	// Start routines for each of the children.
	f := func(c closer.Closer) {
		select {
		case <-c.ClosingChan():
			_ = c.CloseAndDone()
		case <-time.After(time.Second):
			t.Fatal("routine timed out")
		}
	}
	go f(c1)
	go f(c2)
	go f(cc1)
	go f(cc1)

	// Close the parent. This should close c1 and c2, before p closes.
	// c1 should close cc1 before it closes itself.
	err := p.Close()
	require.NoError(t, err)
}

func TestCloser_TwoWay(t *testing.T) {
	// Simple test case.
	t.Run("TwoWay - CloseFunc", testTwoWayCloseFunc)

	// Complex test case.
	t.Run("TwoWay - Routines", testTwoWayRoutines)
}

func testTwoWayCloseFunc(t *testing.T) {
	t.Parallel()

	// A closer chain with only two-way closers.
	p := closer.New()
	c1 := p.TwoWay()
	c2 := c1.TwoWay()

	p.OnClose(func() error {
		require.True(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.True(t, c1.IsClosing())
		require.True(t, c1.IsClosed())

		require.True(t, c2.IsClosing())
		require.True(t, c2.IsClosed())
		return nil
	})
	c1.OnClose(func() error {
		// We close the child, so the parent and the other children must close first.
		require.False(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.True(t, c1.IsClosing())
		require.False(t, c1.IsClosed())

		require.True(t, c2.IsClosing())
		require.True(t, c2.IsClosed())
		return nil
	})
	c2.OnClose(func() error {
		// We close the child, so the parent and the other children must close first.
		require.False(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.False(t, c1.IsClosing())
		require.False(t, c1.IsClosed())

		require.True(t, c2.IsClosing())
		require.False(t, c2.IsClosed())
		return nil
	})

	// Close the lowest child now.
	err := c2.Close()
	require.NoError(t, err)

	// Repeat the test, but this time close the parent.
	p = closer.New()
	c1 = p.TwoWay()
	c2 = c1.TwoWay()

	p.OnClose(func() error {
		require.True(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.True(t, c1.IsClosing())
		require.True(t, c1.IsClosed())

		require.True(t, c2.IsClosing())
		require.True(t, c2.IsClosed())
		return nil
	})
	c1.OnClose(func() error {
		// We close the child, so the parent and the other children must close first.
		require.True(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.True(t, c1.IsClosing())
		require.False(t, c1.IsClosed())

		require.True(t, c2.IsClosing())
		require.True(t, c2.IsClosed())
		return nil
	})
	c2.OnClose(func() error {
		// We close the child, so the parent and the other children must close first.
		require.True(t, p.IsClosing())
		require.False(t, p.IsClosed())

		require.True(t, c1.IsClosing())
		require.False(t, c1.IsClosed())

		require.True(t, c2.IsClosing())
		require.False(t, c2.IsClosed())
		return nil
	})

	// Close the parent this time.
	err = p.Close()
	require.NoError(t, err)
	require.True(t, p.IsClosed())
}

func testTwoWayRoutines(t *testing.T) {
	t.Parallel()

	// One parent with 2 direct, two-way children.
	p := closer.New()
	c1 := p.TwoWay()
	c2 := p.TwoWay()

	p.OnClose(func() error {
		// Both children must be closed
		require.True(t, c1.IsClosed())
		require.True(t, c2.IsClosed())
		return nil
	})

	// This is a child of the first child. This closer
	// will be closed.
	cc1 := c1.TwoWay(func() error {
		require.False(t, p.IsClosed())
		require.False(t, c1.IsClosed())
		require.False(t, c2.IsClosed())
		return nil
	})

	c1.OnClose(func() error {
		require.True(t, cc1.IsClosed())
		require.False(t, p.IsClosed())
		require.False(t, c2.IsClosed())
		return nil
	})

	p.AddWaitGroup(1)
	c1.AddWaitGroup(1)
	c2.AddWaitGroup(1)

	// Start a routine for each of the children.
	f := func(c closer.Closer) {
		select {
		case <-c.ClosingChan():
			_ = c.CloseAndDone()
		case <-time.After(time.Second):
			t.Fatal("routine timed out")
		}
	}
	go f(c1)
	go f(c2)
	go f(p)

	// Close the lowest child. This should close c1, c2 and p.
	err := c1.Close()
	require.NoError(t, err)
}
