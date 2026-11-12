package service

import (
	"context"
	"fmt"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

// event is an event to publish: its type (the topic), subject and data.
type event struct {
	typ     string
	subject string
	data    any
}

// publish publishes the events in order, stopping at the first failure. The
// change they describe is already committed, so the publication is not
// canceled with the request.
func (s *Service) publish(ctx context.Context, evs ...event) error {
	ctx = context.WithoutCancel(ctx)
	for i := range evs {
		ev := &evs[i]
		msg, err := pubsub.Encode(pubsub.NewEnvelope(ev.typ, events.SourceSubtitles, ev.subject, ev.data))
		if err == nil {
			err = s.pub.Publish(ctx, msg)
		}
		if err != nil {
			return fmt.Errorf("publish %s of %s: %w", ev.typ, ev.subject, err)
		}
	}
	return nil
}

func subtitleAddedEvent(m *Match) event {
	return event{typ: events.SubtitleAddedV1, subject: m.ID.String(), data: events.SubtitleEventV1{
		SubtitleID:        m.ID,
		ItemID:            m.ItemID,
		ItemType:          m.ItemType,
		FileID:            m.FileID,
		Format:            m.Format,
		Language:          m.Language,
		Title:             m.Title,
		IsForced:          m.Forced,
		IsHearingImpaired: m.HearingImpaired,
		Source:            m.Source,
	}}
}

func subtitleRemovedEvent(m *Match) event {
	return event{typ: events.SubtitleRemovedV1, subject: m.ID.String(), data: events.SubtitleRemovedEventV1{
		SubtitleID: m.ID,
		ItemID:     m.ItemID,
		FileID:     m.FileID,
	}}
}
