/*
 * closer - A simple, thread-safe closer
 *
 * The MIT License (MIT)
 *
 * Copyright (c) 2020 Roland Singer <roland.singer[at]desertbit.com>
 * Copyright (c) 2020 Sebastian Borchers <sebastian[at]desertbit.com>
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
	"testing"

	"github.com/desertbit/closer/v3"
)

var err error

func BenchmarkCloser_CloserOneWay(b *testing.B) {
	b.Run("1P100C-CloseP", benchmarkCloserOneWay1P100CCloseP)
	b.Run("1P100C-CloseC", benchmarkCloserOneWay1P100CCloseC)
	b.Run("1P100C10CC-CloseP", benchmarkCloserOneWay1P100C10CCCloseP)
	b.Run("1P100C10CC-CloseC", benchmarkCloserOneWay1P100C10CCCloseC)
	b.Run("1P100C10CC-CloseCC", benchmarkCloserOneWay1P100C10CCCloseCC)
}

func benchmarkCloserOneWay1P100CCloseP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := closer.New()
		for j := 0; j < 100; j++ {
			_ = p.CloserOneWay()
		}
		b.StartTimer()

		err = p.Close()
	}
}

func benchmarkCloserOneWay1P100CCloseC(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := closer.New()
		cs := make([]closer.Closer, 100)
		for j := 0; j < 100; j++ {
			cs[j] = p.CloserOneWay()
		}
		b.StartTimer()

		for j := 0; j < 100; j++ {
			err = cs[j].Close()
		}
		err = p.Close()
	}
}

func benchmarkCloserOneWay1P100C10CCCloseP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := closer.New()
		for j := 0; j < 100; j++ {
			c := p.CloserOneWay()
			for k := 0; k < 10; k++ {
				_ = c.CloserOneWay()
			}
		}
		b.StartTimer()

		err = p.Close()
	}
}

func benchmarkCloserOneWay1P100C10CCCloseC(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := closer.New()
		cs := make([]closer.Closer, 100)
		for j := 0; j < 100; j++ {
			cs[j] = p.CloserOneWay()
			for k := 0; k < 10; k++ {
				_ = cs[j].CloserOneWay()
			}
		}
		b.StartTimer()

		for j := 0; j < 100; j++ {
			err = cs[j].Close()
		}
		err = p.Close()
	}
}

func benchmarkCloserOneWay1P100C10CCCloseCC(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p := closer.New()
		cs := make([]closer.Closer, 100)
		ccs := make([]closer.Closer, 100*10)
		for j := 0; j < 100; j++ {
			cs[j] = p.CloserOneWay()
			for k := 0; k < 10; k++ {
				ccs[j*10+k] = cs[j].CloserOneWay()
			}
		}
		b.StartTimer()

		for k := 0; k < 100*10; k++ {
			err = ccs[k].Close()
		}
		for j := 0; j < 100; j++ {
			err = cs[j].Close()
		}
		err = p.Close()
	}
}
