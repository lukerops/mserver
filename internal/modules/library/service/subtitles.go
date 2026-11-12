package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/library/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// SubtitleTopics are the topics HandleSubtitleEvent must be subscribed to.
var SubtitleTopics = []string{
	events.SubtitleAddedV1,
	events.SubtitleRemovedV1,
}

// HandleSubtitleEvent links the subtitles matched by the subtitles module
// to their movies and episodes (one of SubtitleTopics). It is idempotent.
//
// The subtitles are only kept with the item: they do not publish a movie or
// episode updated event, nor count in the subtitle languages used to
// filter the lists (those are the ones of the streams embedded in the
// file).
func (s *Service) HandleSubtitleEvent(ctx context.Context, msg pubsub.Message) error {
	switch msg.Topic {
	case events.SubtitleAddedV1:
		e, err := pubsub.Decode[events.SubtitleEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		sub := ExternalSubtitle{
			ID:              e.Data.SubtitleID,
			FileID:          e.Data.FileID,
			Format:          e.Data.Format,
			Language:        e.Data.Language,
			Title:           e.Data.Title,
			Forced:          e.Data.IsForced,
			HearingImpaired: e.Data.IsHearingImpaired,
			Source:          e.Data.Source,
		}
		return s.updateSubtitles(ctx, e.Data.ItemType, e.Data.ItemID, func(subs []ExternalSubtitle) []ExternalSubtitle {
			return withSubtitle(subs, sub)
		})
	case events.SubtitleRemovedV1:
		e, err := pubsub.Decode[events.SubtitleRemovedEventV1](msg)
		if err != nil {
			return s.dropInvalid(ctx, msg, err)
		}
		// The removed event does not tell the type of the item: it is a
		// movie or an episode, never both, so try both.
		for _, typ := range []string{events.ItemTypeMovie, events.ItemTypeEpisode} {
			err := s.updateSubtitles(ctx, typ, e.Data.ItemID, func(subs []ExternalSubtitle) []ExternalSubtitle {
				return withoutSubtitle(subs, e.Data.SubtitleID)
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// updateSubtitles applies change to the external subtitles of a movie or
// episode and saves them, if they changed. Unknown items are ignored: the
// subtitle will be seen again with a resync of the subtitles.
func (s *Service) updateSubtitles(ctx context.Context, itemType string, id uuid.UUID, change func([]ExternalSubtitle) []ExternalSubtitle) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		current []ExternalSubtitle
		save    func(subs []ExternalSubtitle, at time.Time) error
	)
	switch itemType {
	case events.ItemTypeMovie:
		m, err := s.store.GetMovie(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return s.subtitleIgnored(ctx, "not cataloged", itemType, id)
		}
		if err != nil {
			return err
		}
		current = m.ExternalSubtitles
		save = func(subs []ExternalSubtitle, at time.Time) error {
			return s.store.UpdateMovieSubtitles(ctx, id, subs, at)
		}
	case events.ItemTypeEpisode:
		e, err := s.store.GetEpisode(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return s.subtitleIgnored(ctx, "not cataloged", itemType, id)
		}
		if err != nil {
			return err
		}
		current = e.ExternalSubtitles
		save = func(subs []ExternalSubtitle, at time.Time) error {
			return s.store.UpdateEpisodeSubtitles(ctx, id, subs, at)
		}
	default:
		return s.subtitleIgnored(ctx, "unknown item type", itemType, id)
	}

	subs := change(slices.Clone(current))
	if slices.Equal(subs, current) {
		return s.subtitleIgnored(ctx, "unchanged", itemType, id)
	}
	if err := save(subs, time.Now().UTC()); err != nil {
		return err
	}
	s.log.DebugContext(ctx, "library: external subtitles updated",
		slog.String("item_type", itemType),
		slog.String("item_id", id.String()),
		slog.Int("subtitles", len(subs)),
	)
	return nil
}

func (s *Service) subtitleIgnored(ctx context.Context, reason, itemType string, id uuid.UUID) error {
	s.log.DebugContext(ctx, "library: subtitle ignored",
		slog.String("reason", reason),
		slog.String("item_type", itemType),
		slog.String("item_id", id.String()),
	)
	return nil
}

// withSubtitle returns subs with sub, replacing the one with its ID, in
// the stable order of the store.
func withSubtitle(subs []ExternalSubtitle, sub ExternalSubtitle) []ExternalSubtitle {
	subs = withoutSubtitle(subs, sub.ID)
	subs = append(subs, sub)
	store.SortSubtitles(subs)
	return subs
}

// withoutSubtitle returns subs without the one with the ID.
func withoutSubtitle(subs []ExternalSubtitle, id uuid.UUID) []ExternalSubtitle {
	return slices.DeleteFunc(subs, func(s ExternalSubtitle) bool { return s.ID == id })
}
