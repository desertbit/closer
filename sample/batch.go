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
