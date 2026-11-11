package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/lukerops/mserver/internal/events"
)

// fileAnalyzed saves the media info of the movie or episode of a file, from
// the results of ffprobe version 1. Other analyses and files that are not
// cataloged are ignored.
func (s *Service) fileAnalyzed(ctx context.Context, a *events.FileAnalyzedEventV1) error {
	if a.Analyzer != events.AnalyzerFFprobe {
		s.log.DebugContext(ctx, "library: analysis ignored",
			slog.String("reason", "unsupported analyzer"),
			slog.String("analyzer", a.Analyzer),
			slog.String("file_id", a.FileID.String()),
		)
		return nil
	}
	if a.Version != 1 {
		s.log.WarnContext(ctx, "library: unsupported analysis version",
			slog.String("analyzer", a.Analyzer),
			slog.Int("version", a.Version),
			slog.String("file_id", a.FileID.String()),
		)
		return nil
	}
	var result events.FFprobeResultV1
	if err := json.Unmarshal(a.Result, &result); err != nil {
		s.log.WarnContext(ctx, "library: invalid analysis dropped",
			slog.String("analyzer", a.Analyzer),
			slog.String("file_id", a.FileID.String()),
			slog.String("error", err.Error()),
		)
		return nil
	}
	info := mediaInfo(&result)

	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.findMovie(ctx, a.FileID)
	if err != nil {
		return err
	}
	e, err := s.findEpisode(ctx, a.FileID)
	if err != nil {
		return err
	}
	if m == nil && e == nil {
		s.log.DebugContext(ctx, "library: analysis ignored",
			slog.String("reason", "not cataloged"),
			slog.String("file_id", a.FileID.String()),
		)
		return nil
	}

	now := time.Now().UTC()
	if m != nil {
		if m.MediaInfo.Equal(info) {
			s.logAnalysisUnchanged(ctx, "movie_id", m.ID.String(), a)
		} else {
			if err := s.store.UpdateMovieMediaInfo(ctx, m.ID, info, now); err != nil {
				return err
			}
			m.MediaInfo, m.UpdatedAt = info, now
			s.publish(ctx, movieEvent(events.MovieUpdatedV1, m))
			s.logAnalysisApplied(ctx, "movie_id", m.ID.String(), info, a)
		}
	}
	if e != nil {
		if e.MediaInfo.Equal(info) {
			s.logAnalysisUnchanged(ctx, "episode_id", e.ID.String(), a)
		} else {
			if err := s.store.UpdateEpisodeMediaInfo(ctx, e.ID, info, now); err != nil {
				return err
			}
			e.MediaInfo, e.UpdatedAt = info, now
			s.publish(ctx, episodeEvent(events.EpisodeUpdatedV1, e))
			s.logAnalysisApplied(ctx, "episode_id", e.ID.String(), info, a)
		}
	}
	return nil
}

func (s *Service) logAnalysisUnchanged(ctx context.Context, key, id string, a *events.FileAnalyzedEventV1) {
	s.log.DebugContext(ctx, "library: analysis ignored",
		slog.String("reason", "unchanged"),
		slog.String(key, id),
		slog.String("file_id", a.FileID.String()),
	)
}

func (s *Service) logAnalysisApplied(ctx context.Context, key, id string, info *MediaInfo, a *events.FileAnalyzedEventV1) {
	s.log.DebugContext(ctx, "library: analysis applied",
		slog.String(key, id),
		slog.String("file_id", a.FileID.String()),
		slog.Int("streams", len(info.Streams)),
		slog.String("container", info.Container),
		slog.Int64("duration_ms", info.DurationMs),
	)
}

// mediaInfo converts an ffprobe result to the media info of the catalog.
func mediaInfo(r *events.FFprobeResultV1) *MediaInfo {
	info := &MediaInfo{
		Container:  r.Container,
		DurationMs: r.DurationMs,
		BitRate:    r.BitRate,
		Size:       r.Size,
		Streams:    make([]Stream, len(r.Streams)),
	}
	for i := range r.Streams {
		// Same fields: the catalog keeps everything ffprobe reports.
		info.Streams[i] = Stream(r.Streams[i])
	}
	return info
}
