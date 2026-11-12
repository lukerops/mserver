package handler

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

func toLibrary(l *service.Library) *libraryv1.Library {
	ids := make([]string, len(l.LocationIDs))
	for i := range l.LocationIDs {
		ids[i] = l.LocationIDs[i].String()
	}
	return &libraryv1.Library{
		Id:          l.ID.String(),
		Name:        l.Name,
		MediaType:   l.MediaType,
		LocationIds: ids,
		CreatedAt:   timestamppb.New(l.CreatedAt),
	}
}

func toMovie(m *service.Movie) *libraryv1.Movie {
	return &libraryv1.Movie{
		Id:         m.ID.String(),
		LibraryId:  m.LibraryID.String(),
		FileId:     m.FileID.String(),
		LocationId: m.LocationID.String(),
		Path:       m.Path,
		Title:      m.Title,
		SortTitle:  m.SortTitle,
		Year:       int32(m.Year),
		MediaInfo:  toMediaInfo(m.MediaInfo),
		CreatedAt:  timestamppb.New(m.CreatedAt),
		UpdatedAt:  timestamppb.New(m.UpdatedAt),

		ExternalSubtitles: toExternalSubtitles(m.ExternalSubtitles),
	}
}

func toExternalSubtitles(subs []service.ExternalSubtitle) []*libraryv1.ExternalSubtitle {
	out := make([]*libraryv1.ExternalSubtitle, len(subs))
	for i := range subs {
		s := &subs[i]
		out[i] = &libraryv1.ExternalSubtitle{
			Id:              s.ID.String(),
			FileId:          s.FileID.String(),
			Format:          s.Format,
			Language:        s.Language,
			Title:           s.Title,
			Forced:          s.Forced,
			HearingImpaired: s.HearingImpaired,
			Source:          s.Source,
		}
	}
	return out
}

// toMediaInfo returns nil for a file that was not analyzed yet.
func toMediaInfo(info *service.MediaInfo) *libraryv1.MediaInfo {
	if info == nil {
		return nil
	}
	out := &libraryv1.MediaInfo{
		Container:  info.Container,
		DurationMs: info.DurationMs,
		BitRate:    info.BitRate,
		Size:       info.Size,
		Streams:    make([]*libraryv1.Stream, len(info.Streams)),
	}
	for i := range info.Streams {
		s := &info.Streams[i]
		out.Streams[i] = &libraryv1.Stream{
			Index:           int32(s.Index),
			Type:            s.Type,
			Codec:           s.Codec,
			Profile:         s.Profile,
			Level:           int32(s.Level),
			Language:        s.Language,
			Title:           s.Title,
			Default:         s.Default,
			Forced:          s.Forced,
			HearingImpaired: s.HearingImpaired,
			BitRate:         s.BitRate,
			Width:           int32(s.Width),
			Height:          int32(s.Height),
			AspectRatio:     s.AspectRatio,
			FrameRate:       s.FrameRate,
			PixelFormat:     s.PixelFormat,
			BitDepth:        int32(s.BitDepth),
			Interlaced:      s.Interlaced,
			ColorSpace:      s.ColorSpace,
			ColorTransfer:   s.ColorTransfer,
			ColorPrimaries:  s.ColorPrimaries,
			Channels:        int32(s.Channels),
			ChannelLayout:   s.ChannelLayout,
			SampleRate:      int32(s.SampleRate),
		}
	}
	return out
}

func toSeries(s *service.Series) *libraryv1.Series {
	return &libraryv1.Series{
		Id:           s.ID.String(),
		LibraryId:    s.LibraryID.String(),
		Title:        s.Title,
		SortTitle:    s.SortTitle,
		Year:         int32(s.Year),
		SeasonCount:  int32(s.SeasonCount),
		EpisodeCount: int32(s.EpisodeCount),
		CreatedAt:    timestamppb.New(s.CreatedAt),
	}
}

func toSeason(s *service.Season) *libraryv1.Season {
	return &libraryv1.Season{
		Id:           s.ID.String(),
		SeriesId:     s.SeriesID.String(),
		LibraryId:    s.LibraryID.String(),
		Number:       int32(s.Number),
		SeriesTitle:  s.SeriesTitle,
		SeriesYear:   int32(s.SeriesYear),
		EpisodeCount: int32(s.EpisodeCount),
		CreatedAt:    timestamppb.New(s.CreatedAt),
	}
}

func toEpisode(e *service.Episode) *libraryv1.Episode {
	return &libraryv1.Episode{
		Id:           e.ID.String(),
		LibraryId:    e.LibraryID.String(),
		SeriesId:     e.SeriesID.String(),
		SeasonId:     e.SeasonID.String(),
		FileId:       e.FileID.String(),
		LocationId:   e.LocationID.String(),
		Path:         e.Path,
		Title:        e.Title,
		SeriesTitle:  e.SeriesTitle,
		SeriesYear:   int32(e.SeriesYear),
		SeasonNumber: int32(e.SeasonNumber),
		Number:       toOptional(e.Number),
		EndNumber:    toOptional(e.EndNumber),
		MediaInfo:    toMediaInfo(e.MediaInfo),
		CreatedAt:    timestamppb.New(e.CreatedAt),
		UpdatedAt:    timestamppb.New(e.UpdatedAt),

		ExternalSubtitles: toExternalSubtitles(e.ExternalSubtitles),
	}
}

func toOptional(n *int) *int32 {
	if n == nil {
		return nil
	}
	v := int32(*n)
	return &v
}
