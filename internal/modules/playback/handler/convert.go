package handler

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	playbackv1 "github.com/lukerops/mserver/gen/playback/v1"
	"github.com/lukerops/mserver/internal/modules/playback/service"
)

func toSession(s *service.Session) *playbackv1.Session {
	return &playbackv1.Session{
		Id:         s.ID.String(),
		AccountId:  s.AccountID.String(),
		ProfileId:  s.ProfileID.String(),
		DeviceId:   s.DeviceID.String(),
		ItemId:     s.ItemID.String(),
		FileId:     s.FileID.String(),
		PlayMethod: s.PlayMethod,
		PositionMs: s.PositionMs,
		Paused:     s.Paused,
		WatchedMs:  s.WatchedMs,
		Sequence:   s.Sequence,
		StartedAt:  timestamppb.New(s.StartedAt),
		UpdatedAt:  timestamppb.New(s.UpdatedAt),
		LastSeenAt: timestamppb.New(s.LastSeenAt),
	}
}
