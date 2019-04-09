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

package main

import (
	"fmt"

	"github.com/desertbit/closer"
)

const numberBatchRoutines = 10

type Batch struct {
	closer.Closer
}

func NewBatch(parentCloser closer.Closer) *Batch {
	b := &Batch{
		Closer: parentCloser,
	}
	// Print a message once the batch closes.
	b.OnClose(func() error {
		fmt.Println("batch closing")
		return nil
	})
	return b
}

func (b *Batch) Run() {
	// Fire up several routines and make sure
	b.Closer.AddWaitGroup(numberBatchRoutines)
	for i := 0; i < numberBatchRoutines; i++ {
		go b.workRoutine()
	}

	fmt.Println("batch up and running...")
}

func (b *Batch) workRoutine() {
	// If one work routine dies, we let the others continue their work,
	// so do not close on defer here, but still decrement the wait group.
	defer b.Done()

	// Normally, some work is performed here...
	<-b.ClosingChan()
	fmt.Println("batch routine shutting down")
}
