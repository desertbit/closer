package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/desertbit/closer"
)

type App struct {
	closer.Closer
}

func NewApp() *App {
	return &App{
		Closer: closer.New(),
	}
}

func (a *App) Run() {
	// Create the batch service.
	// The batch may fail, but we do not want it to crash our whole
	// application, therefore, we use a OneWay closer, which closes
	// the batch when the app closes, but not vice versa.
	batch := NewBatch(a.OneWay())
	batch.Run()

	// Create the server.
	// When the server fails, our application should cease to exist.
	// Use a TwoWay closer, so that the app and server close each other
	// when one encounters an error.
	server := NewServer(a.TwoWay())
	server.Run()

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
