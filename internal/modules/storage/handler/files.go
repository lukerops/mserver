package handler

import (
	"context"

	"connectrpc.com/connect/v2"

	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/internal/modules/storage/service"
)

// maxListLimit is the largest page of ListFiles. It is also the page when
// the request does not set a limit: collections are never unbounded.
const maxListLimit = 1000

func (h *Handler) GetFile(ctx context.Context, req *storagev1.GetFileRequest) (*storagev1.GetFileResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	f, err := h.svc.GetFile(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.GetFileResponse{File: toFile(f)}, nil
}

func (h *Handler) ListFiles(ctx context.Context, req *storagev1.ListFilesRequest) (*storagev1.ListFilesResponse, error) {
	location, err := parseID("location_id", req.GetLocationId())
	if err != nil {
		return nil, err
	}
	if req.GetLimit() < 0 || req.GetOffset() < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "limit and offset must not be negative")
	}
	limit := int(req.GetLimit())
	if limit == 0 || limit > maxListLimit {
		limit = maxListLimit
	}
	files, err := h.svc.ListFiles(ctx, location, service.Page{Limit: limit, Offset: int(req.GetOffset())})
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &storagev1.ListFilesResponse{Files: make([]*storagev1.File, len(files))}
	for i := range files {
		resp.Files[i] = toFile(&files[i])
	}
	return resp, nil
}

func (h *Handler) ListSiblings(ctx context.Context, req *storagev1.ListSiblingsRequest) (*storagev1.ListSiblingsResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	siblings, err := h.svc.ListSiblings(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &storagev1.ListSiblingsResponse{Siblings: make([]*storagev1.Sibling, len(siblings))}
	for i := range siblings {
		resp.Siblings[i] = toSibling(&siblings[i])
	}
	return resp, nil
}

func (h *Handler) ResolveFile(ctx context.Context, req *storagev1.ResolveFileRequest) (*storagev1.ResolveFileResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	p, err := h.svc.Resolve(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.ResolveFileResponse{Url: h.baseURL + p}, nil
}
