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

	"github.com/desertbit/closer/v3"
)

const numberListenRoutines = 5

type server struct {
	closer.Closer
}

func newServer(cl closer.Closer) *server {
	s := &server{
		Closer: cl,
	}
	s.OnClose(func() error {
		fmt.Println("server closing")
		return nil
	})
	return s
}

func (s *server) run() {
	// Fire up several routines and make sure our closer waits for each of them when closing.
	s.Closer.CloserAddWait(numberListenRoutines)
	for i := 0; i < numberListenRoutines; i++ {
		go s.listenRoutine()
	}

	fmt.Println("server up and running...")
}

func (s *server) listenRoutine() {
	// When a listen routine dies, this is critical for the server and it will take down
	// the whole server with it.
	defer s.CloseAndDone_()

	// Normally, some work is performed here...
	<-s.ClosingChan()
	fmt.Println("server listen routine shutting down")
}
