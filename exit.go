package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"
)

func handleGracefulExit() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGINT,
		syscall.SIGTERM)

	go func() {
		s := <-sigc
		log.Info().Msgf("got %s, exiting", s)
		// The program doesn't really have anything to clean up so this should be fine
		os.Exit(1)
	}()
}
