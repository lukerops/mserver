package handler

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	storagev1 "github.com/lukerops/mserver/gen/storage/v1"
	"github.com/lukerops/mserver/internal/modules/storage/service"
)

func toLocation(l *service.Location) *storagev1.Location {
	return &storagev1.Location{
		Id:        l.ID.String(),
		Driver:    l.Driver,
		Options:   l.Options,
		CreatedAt: timestamppb.New(l.CreatedAt),
	}
}

func toFile(f *service.File) *storagev1.File {
	return &storagev1.File{
		Id:         f.ID.String(),
		LocationId: f.LocationID.String(),
		Path:       f.Path,
		Size:       f.Size,
		ModTime:    timestamppb.New(f.ModTime),
		Inode:      f.Inode,
		MimeType:   service.MimeType(f.Path),
	}
}

func toSibling(s *service.Sibling) *storagev1.Sibling {
	return &storagev1.Sibling{FileId: s.FileID.String(), Path: s.Path, MimeType: service.MimeType(s.Path)}
}

func toScanResult(r *service.ScanResult) *storagev1.ScanResult {
	return &storagev1.ScanResult{
		Added:   int32(r.Added),
		Changed: int32(r.Changed),
		Moved:   int32(r.Moved),
		Removed: int32(r.Removed),
	}
}
