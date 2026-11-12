package service

import (
	"context"
	"log/slog"
	"path"
	"slices"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
)

// dirKey is a directory of a location, where the subtitles are matched.
type dirKey struct {
	location uuid.UUID
	dir      string
}

func (k dirKey) String() string { return k.location.String() + ":" + k.dir }

// dirOf returns the directory of a path relative to a location.
func dirOf(location uuid.UUID, p string) dirKey {
	return dirKey{location: location, dir: path.Dir(p)}
}

// desiredMatches returns the subtitle files matched with the items of the
// same directory: a subtitle matches the items whose file name (without
// extension) it starts with (see tagsAfterStem). When it matches several,
// only the ones with the longest name count ("Movie.2.en.srt" belongs to
// "Movie.2.mkv", not to "Movie.mkv"); items with the same name (the same
// video in two containers) all match.
func desiredMatches(items []Item, files []SubtitleFile) []Match {
	var matches []Match
	for i := range files {
		f := &files[i]
		subtitleStem := stemOf(f.Path)

		best := 0
		for j := range items {
			if _, ok := tagsAfterStem(stemOf(items[j].Path), subtitleStem); ok {
				best = max(best, len(stemOf(items[j].Path)))
			}
		}
		for j := range items {
			it := &items[j]
			itemStem := stemOf(it.Path)
			rest, ok := tagsAfterStem(itemStem, subtitleStem)
			if !ok || len(itemStem) != best {
				continue
			}
			t := parseTags(rest)
			matches = append(matches, Match{
				ID:              subtitleID(it.ID, f.FileID),
				ItemID:          it.ID,
				ItemType:        it.Type,
				FileID:          f.FileID,
				LocationID:      f.LocationID,
				Dir:             f.Dir,
				Format:          f.Format,
				Language:        t.Language,
				Title:           t.Title,
				Forced:          t.Forced,
				HearingImpaired: t.HearingImpaired,
				Source:          events.SubtitleSourceLocal,
			})
		}
	}
	return matches
}

// reconcile recalculates the matches of the directories from the files and
// items known, saves the difference with the stored ones and publishes it:
// removed events for the matches that are gone, then added events for the
// new matches and the ones whose attributes changed. The caller holds s.mu.
//
// Recalculating the whole directory, instead of reacting to each event,
// makes the result independent of the order the events arrive in.
func (s *Service) reconcile(ctx context.Context, dirs ...dirKey) error {
	var evs []event
	for i, k := range dirs {
		if slices.Contains(dirs[:i], k) {
			continue
		}
		dirEvs, err := s.reconcileDir(ctx, k)
		if err != nil {
			return err
		}
		evs = append(evs, dirEvs...)
	}
	if err := s.publish(ctx, evs...); err != nil {
		s.log.ErrorContext(ctx, "subtitles: publish events failed, a resync reconciles the consumers",
			slog.String("error", err.Error()),
		)
	}
	return nil
}

func (s *Service) reconcileDir(ctx context.Context, k dirKey) ([]event, error) {
	items, err := s.store.ListItems(ctx, k.location, k.dir)
	if err != nil {
		return nil, err
	}
	files, err := s.store.ListSubtitleFiles(ctx, k.location, k.dir)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.ListDirMatches(ctx, k.location, k.dir)
	if err != nil {
		return nil, err
	}

	storedByID := make(map[uuid.UUID]*Match, len(stored))
	for i := range stored {
		storedByID[stored[i].ID] = &stored[i]
	}

	var (
		saved   []Match
		removed []uuid.UUID
		added   []event
		gone    []event
	)
	for _, m := range desiredMatches(items, files) {
		old, ok := storedByID[m.ID]
		if ok {
			delete(storedByID, m.ID)
			if *old == m {
				continue
			}
		}
		saved = append(saved, m)
		added = append(added, subtitleAddedEvent(&m))
	}
	for i := range stored {
		if m, ok := storedByID[stored[i].ID]; ok {
			removed = append(removed, m.ID)
			gone = append(gone, subtitleRemovedEvent(m))
		}
	}
	if len(saved) == 0 && len(removed) == 0 {
		return nil, nil
	}

	if err := s.store.ApplyMatches(ctx, saved, removed); err != nil {
		return nil, err
	}
	s.log.DebugContext(ctx, "subtitles: matches updated",
		slog.String("directory", k.String()),
		slog.Int("items", len(items)),
		slog.Int("files", len(files)),
		slog.Int("added", len(saved)),
		slog.Int("removed", len(removed)),
	)
	return append(gone, added...), nil
}
