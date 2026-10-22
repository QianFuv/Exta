package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"exta/internal/service"
	"exta/internal/store"
)

func runWorkers(ctx context.Context, core *service.Service) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	completed := make(chan error, 2)
	go func() { completed <- core.Research.Run(ctx) }()
	go func() { completed <- runRetention(ctx, core.Store) }()
	first := <-completed
	cancel()
	return errors.Join(first, <-completed)
}

func runRetention(ctx context.Context, state *store.Store) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := state.Cleanup(bounded, 100)
		cancel()
		if err != nil {
			if ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, sql.ErrTxDone)) {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
