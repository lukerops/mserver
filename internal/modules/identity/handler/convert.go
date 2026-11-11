package handler

import (
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
	"github.com/lukerops/mserver/internal/modules/identity/service"
)

func toAccount(a *service.Account) *identityv1.Account {
	return &identityv1.Account{
		Id:        a.ID.String(),
		Username:  a.Username,
		Email:     a.Email,
		IsAdmin:   a.IsAdmin,
		Disabled:  a.Disabled,
		CreatedAt: timestamppb.New(a.CreatedAt),
		UpdatedAt: timestamppb.New(a.UpdatedAt),
	}
}

func toProfile(p *service.Profile) *identityv1.Profile {
	return &identityv1.Profile{
		Id:          p.ID.String(),
		AccountId:   p.AccountID.String(),
		Handle:      p.Handle,
		DisplayName: p.DisplayName,
		CreatedAt:   timestamppb.New(p.CreatedAt),
		UpdatedAt:   timestamppb.New(p.UpdatedAt),
	}
}

func toSession(s *service.Session) *identityv1.Session {
	sess := &identityv1.Session{
		Id:        s.ID.String(),
		AccountId: s.AccountID.String(),
		DeviceId:  s.DeviceID.String(),
		CreatedAt: timestamppb.New(s.CreatedAt),
	}
	// The profile is empty until one is selected.
	if s.ProfileID != uuid.Nil {
		sess.ProfileId = s.ProfileID.String()
	}
	return sess
}
