package main

func main() {
	// Create the main application.
	app := NewApp()

	// Run the application.
	app.Run()

	// Wait until the app closed completely.
	<-app.ClosedChan()
}
