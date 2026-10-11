package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/skratchdot/open-golang/open"
)

type Args struct {
	Open bool
	Port string
}

// Run serves until the context is cancelled (ctrl+c).
func Run(ctx context.Context, args Args) error {
	// TODO: should this be done after server.Start? Maybe as a defer?
	if args.Open {
		open.Start("http://localhost:" + args.Port)
	}

	server, err := NewServer()
	if err != nil {
		return err
	}

	errs := make(chan error, 1)
	go func() { errs <- server.Start(":" + args.Port) }()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		return fmt.Errorf("stop the server: %w", err)
	}
	if err := <-errs; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
