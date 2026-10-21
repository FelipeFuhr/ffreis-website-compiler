// Package mediameta classifies post/page media by path and reads intrinsic
// dimensions out of encoded image bytes.
//
// It exists so the blog-post renderer (internal/posts) and the social-image
// builder (internal/buildcmd) share one decoder registration set and one
// dimension reader. internal/buildcmd imports internal/posts, so a helper
// living in buildcmd cannot be reused by posts without an import cycle —
// hence a third package both can depend on rather than a second copy.
package mediameta

import (
	"bytes"
	"image"
	"path"
	"strings"

	// Register the decoders image.DecodeConfig dispatches on. These are the
	// formats the fleet's sites actually ship; anything else yields
	// ErrUnsupported from DecodeConfig and is handled as "dimensions unknown"
	// rather than as a build failure.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Kind is the broad media class a path belongs to.
type Kind int

const (
	// KindImage is the default: anything not recognised as a video.
	KindImage Kind = iota
	// KindVideo is a path whose extension names a browser-playable video
	// container.
	KindVideo
)

// videoExtensions are the containers <video> can play across current browsers.
// .mov is included because it is what phones and screen recorders emit and
// authors do drop it straight into a post; Safari plays it and Chrome plays
// the common H.264 case.
var videoExtensions = map[string]bool{
	".mp4":  true,
	".webm": true,
	".ogv":  true,
	".ogg":  true,
	".mov":  true,
	".m4v":  true,
}

// posterExtensions are tried, in order, when looking for a video's poster
// frame beside it.
var posterExtensions = []string{".webp", ".jpg", ".jpeg", ".png"}

// KindForPath classifies by extension alone. It never touches the filesystem,
// so it is safe to call on a URL or on a path that does not exist.
func KindForPath(p string) Kind {
	if videoExtensions[strings.ToLower(path.Ext(p))] {
		return KindVideo
	}
	return KindImage
}

// PosterCandidates returns the sibling paths that could hold a poster frame
// for the given video path, in preference order: the same basename with each
// image extension. Callers stat them and use the first that exists.
func PosterCandidates(videoPath string) []string {
	ext := path.Ext(videoPath)
	base := strings.TrimSuffix(videoPath, ext)
	out := make([]string, 0, len(posterExtensions))
	for _, e := range posterExtensions {
		out = append(out, base+e)
	}
	return out
}

// ImageDimensions reads the intrinsic pixel dimensions out of encoded image
// bytes without decoding the full raster. The error is returned rather than
// swallowed so a caller can distinguish "not an image we can read" from a
// genuine zero.
func ImageDimensions(data []byte) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}
