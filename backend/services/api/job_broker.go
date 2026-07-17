package main

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

type jobBroker struct {
	log zerolog.Logger

	mu      sync.RWMutex
	clients map[string]map[chan shared.JobUpdatedEvent]struct{}
}

func newJobBroker(
	ctx context.Context,
	nc *nats.Conn,
	logger zerolog.Logger,
) (*jobBroker, error) {
	broker := &jobBroker{
		log: logger, clients: make(map[string]map[chan shared.JobUpdatedEvent]struct{}),
	}
	subscription, err := nc.Subscribe(shared.SubjectJobUpdated, broker.receive)
	if err != nil {
		return nil, err
	}
	if err := nc.Flush(); err != nil {
		_ = subscription.Unsubscribe()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = subscription.Unsubscribe()
	}()
	return broker, nil
}

func (b *jobBroker) receive(message *nats.Msg) {
	var event shared.JobUpdatedEvent
	if err := json.Unmarshal(message.Data, &event); err != nil {
		b.log.Warn().Err(err).Msg("discard malformed job event")
		return
	}
	if !shared.ValidID(event.JobID) || !shared.ValidID(event.ProjectID) {
		b.log.Warn().Msg("discard job event with invalid ids")
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for client := range b.clients[event.ProjectID] {
		select {
		case client <- event:
		default:
			// WebSocket events are an optimization. Keep the latest transitions
			// flowing instead of blocking every client behind one slow browser.
			select {
			case <-client:
			default:
			}
			select {
			case client <- event:
			default:
			}
		}
	}
}

func (b *jobBroker) subscribe(projectID string) (<-chan shared.JobUpdatedEvent, func()) {
	client := make(chan shared.JobUpdatedEvent, 32)
	b.mu.Lock()
	if b.clients[projectID] == nil {
		b.clients[projectID] = make(map[chan shared.JobUpdatedEvent]struct{})
	}
	b.clients[projectID][client] = struct{}{}
	b.mu.Unlock()

	return client, func() {
		b.mu.Lock()
		delete(b.clients[projectID], client)
		if len(b.clients[projectID]) == 0 {
			delete(b.clients, projectID)
		}
		close(client)
		b.mu.Unlock()
	}
}
