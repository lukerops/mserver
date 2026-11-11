package handler

import (
	"context"

	libraryv1 "github.com/lukerops/mserver/gen/library/v1"
	"github.com/lukerops/mserver/internal/modules/library/service"
)

func (h *Handler) CreateLibrary(ctx context.Context, req *libraryv1.CreateLibraryRequest) (*libraryv1.CreateLibraryResponse, error) {
	locations, err := parseIDs("location_ids", req.GetLocationIds())
	if err != nil {
		return nil, err
	}
	lib, err := h.svc.CreateLibrary(ctx, &service.CreateLibraryRequest{
		Name:        req.GetName(),
		MediaType:   req.GetMediaType(),
		LocationIDs: locations,
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.CreateLibraryResponse{Library: toLibrary(lib)}, nil
}

func (h *Handler) GetLibrary(ctx context.Context, req *libraryv1.GetLibraryRequest) (*libraryv1.GetLibraryResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	lib, err := h.svc.GetLibrary(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.GetLibraryResponse{Library: toLibrary(lib)}, nil
}

func (h *Handler) ListLibraries(ctx context.Context, _ *libraryv1.ListLibrariesRequest) (*libraryv1.ListLibrariesResponse, error) {
	libraries, err := h.svc.ListLibraries(ctx)
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &libraryv1.ListLibrariesResponse{Libraries: make([]*libraryv1.Library, len(libraries))}
	for i := range libraries {
		resp.Libraries[i] = toLibrary(&libraries[i])
	}
	return resp, nil
}

func (h *Handler) DeleteLibrary(ctx context.Context, req *libraryv1.DeleteLibraryRequest) (*libraryv1.DeleteLibraryResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteLibrary(ctx, id); err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.DeleteLibraryResponse{}, nil
}

func (h *Handler) ResyncLibrary(ctx context.Context, req *libraryv1.ResyncLibraryRequest) (*libraryv1.ResyncLibraryResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.ResyncLibrary(ctx, id); err != nil {
		return nil, h.connectError(err)
	}
	return &libraryv1.ResyncLibraryResponse{}, nil
}
