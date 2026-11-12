package handlers

import (
	"net/url"
	"strings"

	"github.com/lukerops/mserver/plugins/api/jellyfin/service"
)

// signSubtitles appends the token of the request to the DeliveryUrl of the
// external subtitle streams, as the Jellyfin server does: clients fetch that
// URL as it is, with no authorization header. It is idempotent, because the
// streams of an item and of its media source share memory.
func signSubtitles(streams []service.MediaStream, token string) {
	if token == "" {
		return
	}
	for i := range streams {
		if u := streams[i].DeliveryUrl; u != "" && !strings.Contains(u, "?") {
			streams[i].DeliveryUrl = u + "?ApiKey=" + url.QueryEscape(token)
		}
	}
}

// signItem signs the subtitle streams of an item and of its media sources
// (see signSubtitles).
func signItem(item *service.Item, token string) {
	signSubtitles(item.MediaStreams, token)
	for i := range item.MediaSources {
		signSubtitles(item.MediaSources[i].MediaStreams, token)
	}
}

// signItems signs the items of a query result (see signSubtitles).
func signItems(items []service.Item, token string) {
	for i := range items {
		signItem(&items[i], token)
	}
}

// signSources signs the subtitle streams of the media sources of a playback
// response (see signSubtitles).
func signSources(sources []service.MediaSource, token string) {
	for i := range sources {
		signSubtitles(sources[i].MediaStreams, token)
	}
}
