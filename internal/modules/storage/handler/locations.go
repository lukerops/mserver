package handler

import (
	"context"

	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/internal/modules/storage/service"
)

func (h *Handler) CreateLocation(ctx context.Context, req *storagev1.CreateLocationRequest) (*storagev1.CreateLocationResponse, error) {
	loc, err := h.svc.CreateLocation(ctx, &service.CreateLocationRequest{
		Driver:  req.GetDriver(),
		Options: req.GetOptions(),
	})
	if err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.CreateLocationResponse{Location: toLocation(loc)}, nil
}

func (h *Handler) GetLocation(ctx context.Context, req *storagev1.GetLocationRequest) (*storagev1.GetLocationResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	loc, err := h.svc.GetLocation(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.GetLocationResponse{Location: toLocation(loc)}, nil
}

func (h *Handler) ListLocations(ctx context.Context, _ *storagev1.ListLocationsRequest) (*storagev1.ListLocationsResponse, error) {
	locations, err := h.svc.ListLocations(ctx)
	if err != nil {
		return nil, h.connectError(err)
	}
	resp := &storagev1.ListLocationsResponse{Locations: make([]*storagev1.Location, len(locations))}
	for i := range locations {
		resp.Locations[i] = toLocation(&locations[i])
	}
	return resp, nil
}

func (h *Handler) DeleteLocation(ctx context.Context, req *storagev1.DeleteLocationRequest) (*storagev1.DeleteLocationResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteLocation(ctx, id); err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.DeleteLocationResponse{}, nil
}

func (h *Handler) ScanLocation(ctx context.Context, req *storagev1.ScanLocationRequest) (*storagev1.ScanLocationResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	res, err := h.svc.ScanLocation(ctx, id)
	if err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.ScanLocationResponse{Result: toScanResult(res)}, nil
}

func (h *Handler) ResyncLocation(ctx context.Context, req *storagev1.ResyncLocationRequest) (*storagev1.ResyncLocationResponse, error) {
	id, err := parseID("id", req.GetId())
	if err != nil {
		return nil, err
	}
	if err := h.svc.ResyncLocation(ctx, id); err != nil {
		return nil, h.connectError(err)
	}
	return &storagev1.ResyncLocationResponse{}, nil
}
