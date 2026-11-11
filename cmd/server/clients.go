package main

import (
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"connectrpc.com/connect/v2/connectinprocess"

	"github.com/lukerops/mserver/gen/storage/v1/storagev1connect"
)

// newStorageClient returns the client of the storage API for the modules
// that use it. With url set (MSERVER_STORAGE_URL) the storage is another
// instance, reached over h2c; otherwise it is the module of this process
// (local), reached through the in-process transport of rpc.
func newStorageClient(rpc *connect.Server, url string, local bool) (storagev1connect.StorageServiceClient, error) {
	var transport connect.Transport
	switch {
	case url != "":
		transport = connecthttp.NewTransport(newH2CClient(), url)
	case local:
		transport = connectinprocess.New(rpc)
	default:
		return nil, errors.New("the storage module is not running here: set MSERVER_STORAGE_URL")
	}

	return storagev1connect.NewStorageServiceClient(connect.NewClient(transport)), nil
}

// newH2CClient returns an HTTP client that speaks HTTP/2 without TLS (h2c),
// for the calls to the other instances. It never uses a proxy: the calls
// are internal.
func newH2CClient() *http.Client {
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	return &http.Client{Transport: &http.Transport{
		Protocols:           &protocols,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}}
}
