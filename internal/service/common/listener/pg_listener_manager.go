package listener

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NotificationHandler defines the signature for processing a notification payload.
type NotificationHandler func(ctx context.Context, pgNotification *pgconn.Notification) error

// CatchUpFunc defines the signature for a catch-up function.
type CatchUpFunc func(ctx context.Context) error

// ChannelConfig holds both the real-time handler and optional catch-up configuration for a channel.
type ChannelConfig struct {
	Handler         NotificationHandler
	CatchUpFunc     CatchUpFunc
	CatchUpInterval time.Duration
}

// Manager manages the registration and handling of PostgreSQL notification channels.
type Manager struct {
	pool     *pgxpool.Pool
	channels map[string]ChannelConfig
	wg       sync.WaitGroup
}

// NewListenerManager initializes a new ListenerManager.
func NewListenerManager(pool *pgxpool.Pool) *Manager {
	return &Manager{
		pool:     pool,
		channels: make(map[string]ChannelConfig),
	}
}

// RegisterListener adds a new channel along with its notification handler and optional catch-up configuration.
// If no catch-up is needed for a channel, pass nil for catchUp and 0 for interval.
func (lm *Manager) RegisterListener(channel string, handler NotificationHandler, catchUp CatchUpFunc, interval time.Duration) {
	lm.channels[channel] = ChannelConfig{
		Handler:         handler,
		CatchUpFunc:     catchUp,
		CatchUpInterval: interval,
	}
}

// StartListeners begins listening on all registered channels.
// It spawns one goroutine per channel for real-time listening and, if configured, one for catch-up.
func (lm *Manager) StartListeners(ctx context.Context) error {
	for channel, config := range lm.channels {
		// Start real-time listener.
		lm.wg.Add(1)
		go lm.listenChannel(ctx, channel, config.Handler)

		// Start catch-up routine if a catch-up function is provided.
		if config.CatchUpFunc != nil && config.CatchUpInterval > 0 {
			lm.wg.Add(1)
			go lm.startChannelCatchUp(ctx, channel, config.CatchUpInterval, config.CatchUpFunc)
		}
	}
	return nil
}

// listenChannel handles notifications for a specific channel.
func (lm *Manager) listenChannel(ctx context.Context, channel string, handler NotificationHandler) {
	defer lm.wg.Done()
	for {
		// listenAndProcess returns on error (it could simply be a shutdown signal or fatal database error)
		if err := lm.listenAndProcess(ctx, channel, handler); err != nil {
			slog.Error("Error listening to channel", "channel", channel, "error", err)
			// Wait before retrying to avoid busy-looping.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}
}

// listenAndProcess acquires a connection, sets up LISTEN, and processes notifications.
func (lm *Manager) listenAndProcess(ctx context.Context, channel string, handler NotificationHandler) error {
	conn, err := lm.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for %s: %w", channel, err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, fmt.Sprintf("LISTEN %s", channel)); err != nil {
		return fmt.Errorf("failed to set up listener on %s: %w", channel, err)
	}

	slog.Info("Listening for notifications", "channel", channel)
	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				slog.Info("Listener is shutting down", "pg_channel", channel)
				return nil
			}

			return fmt.Errorf("failed waiting for notification on %s: %w", channel, err)
		}
		slog.Debug("Received notification from PG", "channel", channel, "payload", notification.Payload)
		// Process the notification payload using the registered handler.
		if err := handler(ctx, notification); err != nil {
			slog.Error("Failed to process notification", "channel", channel, "error", err)
		}
	}
}

// startChannelCatchUp launches a catch-up polling routine for a specific channel.
func (lm *Manager) startChannelCatchUp(ctx context.Context, channel string, interval time.Duration, catchUp CatchUpFunc) {
	defer lm.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := catchUp(ctx); err != nil {
				slog.Error("Catch-up processing failed", "channel", channel, "error", err)
			}
		}
	}
}

// Wait blocks until all listener goroutines have finished.
func (lm *Manager) Wait() {
	lm.wg.Wait()
}
