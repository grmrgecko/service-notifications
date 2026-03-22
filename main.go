package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/slack-go/slack"
	"gorm.io/gorm"
)

const (
	serviceName        = "service-notifications"
	serviceDescription = "Notifications for church services"
	serviceVersion     = "0.2.1"
)

// App is the global application structure for communicating between servers and storing information.
type App struct {
	flags  *Flags
	config *Config
	db     *gorm.DB
	slack  *slack.Client
	http   *HTTPServer
}

var app *App

func main() {
	app = new(App)
	app.ParseFlags()
	app.ReadConfig()
	app.InitDB()
	app.slack = slack.New(app.config.Slack.APIToken)

	// If update is requested, run updates and end the program.
	if app.flags.Update {
		UpdatePCData()
		UpdateSlackData()
		CreateSlackChannels()
		return
	}

	// Configure the HTTP server.
	app.http = NewHTTPServer()

	// Setup context with cancellation function to allow background services to gracefully stop.
	ctx, ctxCancel := context.WithCancel(context.Background())
	app.http.Start(ctx)

	// Monitor common signals.
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	// Wiat for a signal.
	<-c
	// Stop the HTTP server and end.
	ctxCancel()
}
