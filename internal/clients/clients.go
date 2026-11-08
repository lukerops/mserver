// Package clients groups the clients of every module API. cmd/server
// creates them and hands them to the modules and API plugins, which use
// only the ones they need.
package clients

import (
	identity "github.com/lukerops/mserver/internal/modules/identity/client"
	library "github.com/lukerops/mserver/internal/modules/library/client"
	playback "github.com/lukerops/mserver/internal/modules/playback/client"
	storage "github.com/lukerops/mserver/internal/modules/storage/client"
)

type Clients struct {
	Identity *identity.Client
	Storage  *storage.Client
	Library  *library.Client
	Playback *playback.Client
}
