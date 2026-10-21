package mediameta

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"reflect"
	"testing"
)

func TestKindForPathClassifiesVideoExtensions(t *testing.T) {
	videos := []string{
		"clip.mp4", "clip.webm", "clip.ogv", "clip.ogg", "clip.mov", "clip.m4v",
		"CLIP.MP4", "a/b/clip.WebM", "/blog/x/videos/clip.mp4",
	}
	for _, p := range videos {
		if got := KindForPath(p); got != KindVideo {
			t.Errorf("KindForPath(%q) = %v, want KindVideo", p, got)
		}
	}

	images := []string{
		"a.png", "a.jpg", "a.jpeg", "a.webp", "a.gif", "a.svg", "a.avif",
		"noextension", "a.mp4.png", "", "a.mp3", "a.pdf",
	}
	for _, p := range images {
		if got := KindForPath(p); got != KindImage {
			t.Errorf("KindForPath(%q) = %v, want KindImage", p, got)
		}
	}
}

func TestPosterCandidatesPreferenceOrder(t *testing.T) {
	got := PosterCandidates("/blog/demo/videos/clip.mp4")
	want := []string{
		"/blog/demo/videos/clip.webp",
		"/blog/demo/videos/clip.jpg",
		"/blog/demo/videos/clip.jpeg",
		"/blog/demo/videos/clip.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PosterCandidates mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func TestPosterCandidatesHandlesNoExtension(t *testing.T) {
	got := PosterCandidates("clip")
	if len(got) != 4 || got[0] != "clip.webp" {
		t.Errorf("unexpected candidates for extensionless path: %v", got)
	}
}

func TestImageDimensionsReadsEncodedFormats(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	img.Set(0, 0, color.RGBA{R: 9, A: 255})

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, img, nil); err != nil {
		t.Fatal(err)
	}

	for name, data := range map[string][]byte{"png": pngBuf.Bytes(), "jpeg": jpgBuf.Bytes()} {
		w, h, err := ImageDimensions(data)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
			continue
		}
		if w != 640 || h != 480 {
			t.Errorf("%s: got %dx%d, want 640x480", name, w, h)
		}
	}
}

// The error must be returned rather than swallowed, so a caller can tell
// "not an image we can read" apart from a genuine zero dimension.
func TestImageDimensionsReportsErrorForNonImage(t *testing.T) {
	for name, data := range map[string][]byte{
		"text":  []byte("this is not an image"),
		"empty": {},
		"truncated-png": append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
			[]byte("cut off here")...),
	} {
		w, h, err := ImageDimensions(data)
		if err == nil {
			t.Errorf("%s: expected an error, got %dx%d", name, w, h)
		}
		if w != 0 || h != 0 {
			t.Errorf("%s: expected zero dimensions on error, got %dx%d", name, w, h)
		}
	}
}
