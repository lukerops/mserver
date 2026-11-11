// Package clients groups the clients of every module API. cmd/server
// creates them and hands them to the modules and API plugins, which use
// only the ones they need.
package clients

import (
	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/gen/library/v1/libraryv1connect"
	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
	playback "github.com/lukerops/mserver/internal/modules/playback/client"
)

type Clients struct {
	Identity identityv1connect.IdentityServiceClient
	Storage  storagev1connect.StorageServiceClient
	Library  libraryv1connect.LibraryServiceClient
	Playback *playback.Client
}
