package service

import (
	"mime"
	"path"
	"strings"
)

// DefaultMimeType is the media type of the files whose type is unknown.
const DefaultMimeType = "application/octet-stream"

// mimeTypes are the media types of the extensions that mime.TypeByExtension
// does not know, or answers differently, depending on the system (e.g. .ts
// is often a TypeScript file). Video extensions must be here: they are what
// consumers filter the video files by.
var mimeTypes = map[string]string{
	".3gp":  "video/3gpp",
	".asf":  "video/x-ms-asf",
	".avi":  "video/x-msvideo",
	".divx": "video/divx",
	".flv":  "video/x-flv",
	".m2ts": "video/mp2t",
	".m4v":  "video/mp4",
	".mkv":  "video/x-matroska",
	".mov":  "video/quicktime",
	".mp4":  "video/mp4",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".mts":  "video/mp2t",
	".ogm":  "video/ogg",
	".ogv":  "video/ogg",
	".rmvb": "video/vnd.rn-realvideo",
	".ts":   "video/mp2t",
	".vob":  "video/mpeg",
	".webm": "video/webm",
	".wmv":  "video/x-ms-wmv",

	".mka": "audio/x-matroska",

	".srt": "application/x-subrip",
	".vtt": "text/vtt",
	".ass": "text/x-ssa",
	".ssa": "text/x-ssa",
}

// MimeType is the media type of the file at p, guessed from its extension.
// It is DefaultMimeType when the extension is unknown.
func MimeType(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if typ, ok := mimeTypes[ext]; ok {
		return typ
	}
	if typ := mime.TypeByExtension(ext); typ != "" {
		// Drop the parameters (e.g. "; charset=utf-8").
		base, _, err := mime.ParseMediaType(typ)
		if err == nil {
			return base
		}
	}
	return DefaultMimeType
}
