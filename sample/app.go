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
	"math/rand"
	"time"

	"github.com/desertbit/closer/v3"
)

type app struct {
	closer.Closer
}

func newApp() *app {
	return &app{
		Closer: closer.New(),
	}
}

func (a *app) run() {
	// Create the batch service.
	// The batch may fail, but we do not want it to crash our whole
	// application, therefore, we use a OneWay closer, which closes
	// the batch when the app closes, but not vice versa.
	batch := newBatch(a.CloserOneWay())
	batch.run()

	// Create the server.
	// When the server fails, our application should cease to exist.
	// Use a TwoWay closer, so that the app and server close each other
	// when one encounters an error.
	server := newServer(a.CloserTwoWay())
	server.run()

	// For the sake of the example, close the server or the batch
	// to simulate a failure and look, how the application behaves.
	go func() {
		time.Sleep(time.Second)
		rand.Seed(time.Now().UnixNano())
		r := rand.Intn(2)
		if r == 0 {
			fmt.Println("starting to close batch")
			_ = batch.Close()
			time.Sleep(time.Second * 3)
			fmt.Println("now server closes, but independently")
			_ = server.Close()
		} else {
			fmt.Println("starting to close server")
			_ = server.Close()
		}
	}()
}
