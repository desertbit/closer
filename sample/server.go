package main

import (
	"fmt"

	"github.com/desertbit/closer"
)

const numberListenRoutines = 5

type Server struct {
	closer.Closer
}

func NewServer(parentCloser closer.Closer) *Server {
	s := &Server{
		Closer: parentCloser,
	}
	s.OnClose(func() error {
		fmt.Println("server closing")
		return nil
	})
	return s
}

func (s *Server) Run() {
	// Fire up several routines and make sure our closer waits for each of them when closing.
	s.Closer.AddWaitGroup(numberListenRoutines)
	for i := 0; i < numberListenRoutines; i++ {
		go s.listenRoutine()
	}

	fmt.Println("server up and running...")
}

func (s *Server) listenRoutine() {
	// When a listen routine dies, this is critical for the server and it will take down
	// the whole server with it.
	defer s.CloseAndDone()

	// Normally, some work is performed here...
	<-s.ClosingChan()
	fmt.Println("server listen routine shutting down")
}
