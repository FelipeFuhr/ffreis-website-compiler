package posts

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// encodePNG returns a real PNG of the given size. Hand-written PNG bytes with
// a wrong CRC decode as an error and silently produce "dimensions unknown",
// which looks identical to the feature not working — so the fixtures are
// always encoder-produced.
func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding fixture png: %v", err)
	}
	return buf.Bytes()
}

// renderPost writes a one-post tree with the given body and media files, then
// returns the rendered body HTML. files maps a post-relative path to contents.
func renderPost(t *testing.T, body string, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	postDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(postDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		full := filepath.Join(postDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	md := "---\ntitle: Demo\ndate: \"2026-01-01\"\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(postDir, "index.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPostsDir(root)
	if err != nil {
		t.Fatalf("LoadPostsDir: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 post, got %d", len(loaded))
	}
	return loaded[0].BodyHTML
}

func mustContain(t *testing.T, html, want string) {
	t.Helper()
	if !strings.Contains(html, want) {
		t.Errorf("expected HTML to contain %q\ngot:\n%s", want, html)
	}
}

func mustNotContain(t *testing.T, html, unwanted string) {
	t.Helper()
	if strings.Contains(html, unwanted) {
		t.Errorf("expected HTML NOT to contain %q\ngot:\n%s", unwanted, html)
	}
}

// --- video ----------------------------------------------------------------

// A video destination used to render an <img> pointing at an mp4, which a
// browser shows as a broken image. This is the core regression.
func TestVideoRendersVideoElementNotImage(t *testing.T) {
	html := renderPost(t, "![a clip](./videos/clip.mp4)", map[string][]byte{
		"videos/clip.mp4": []byte("fake mp4 bytes"),
	})
	mustContain(t, html, `<video src="/blog/demo/videos/clip.mp4"`)
	mustNotContain(t, html, "<img")
	mustNotContain(t, html, "./videos/clip.mp4")
	// internal/linkcheck validates <a href> against generated pages only, so
	// a download anchor here would be reported as a broken internal link.
	mustNotContain(t, html, "<a href")
}

func TestVideoCarriesPlaybackAttributes(t *testing.T) {
	html := renderPost(t, "![a clip](./videos/clip.mp4)", map[string][]byte{
		"videos/clip.mp4": []byte("fake mp4 bytes"),
	})
	for _, attr := range []string{"controls", `preload="metadata"`, "playsinline"} {
		mustContain(t, html, attr)
	}
}

func TestVideoAltBecomesAriaLabelAndFallbackText(t *testing.T) {
	html := renderPost(t, "![a clip](./videos/clip.mp4)", map[string][]byte{
		"videos/clip.mp4": []byte("fake mp4 bytes"),
	})
	// <video> has no alt attribute, so the accessible name is aria-label.
	mustContain(t, html, `aria-label="a clip"`)
	mustNotContain(t, html, `alt="a clip"`)
	mustContain(t, html, "a clip (video not supported by this browser)")
}

func TestVideoWithoutAltGetsGenericFallbackAndNoAriaLabel(t *testing.T) {
	html := renderPost(t, "![](./videos/clip.mp4)", map[string][]byte{
		"videos/clip.mp4": []byte("fake mp4 bytes"),
	})
	mustContain(t, html, "This browser cannot play the embedded video.")
	mustNotContain(t, html, "aria-label")
}

// A poster frame beside the video supplies both the pre-play paint and the
// dimensions that stop layout shift; mp4 headers are never parsed.
func TestVideoPosterIsDetectedBesideItAndSuppliesDimensions(t *testing.T) {
	html := renderPost(t, "![a clip](./videos/clip.mp4)", map[string][]byte{
		"videos/clip.mp4": []byte("fake mp4 bytes"),
		"videos/clip.png": encodePNG(t, 1280, 720),
	})
	mustContain(t, html, `poster="/blog/demo/videos/clip.png"`)
	mustContain(t, html, `width="1280" height="720"`)
}

func TestVideoWithoutPosterOmitsPosterAndDimensions(t *testing.T) {
	html := renderPost(t, "![a clip](./videos/clip.mp4)", map[string][]byte{
		"videos/clip.mp4": []byte("fake mp4 bytes"),
	})
	mustNotContain(t, html, "poster=")
	mustNotContain(t, html, "width=")
}

func TestEveryVideoExtensionRendersAsVideo(t *testing.T) {
	for _, ext := range []string{"mp4", "webm", "ogv", "mov", "m4v"} {
		t.Run(ext, func(t *testing.T) {
			html := renderPost(t, "![c](./videos/clip."+ext+")", map[string][]byte{
				"videos/clip." + ext: []byte("fake"),
			})
			mustContain(t, html, "<video")
			mustNotContain(t, html, "<img")
		})
	}
}

// --- images ---------------------------------------------------------------

func TestImageGetsIntrinsicDimensionsFromFile(t *testing.T) {
	html := renderPost(t, "![a](./images/a.png)", map[string][]byte{
		"images/a.png": encodePNG(t, 800, 600),
	})
	mustContain(t, html, `width="800" height="600"`)
}

// A referenced file that is absent or not decodable must degrade to "no
// dimensions", never fail the build — the pre-existing behaviour.
func TestUndecodableOrMissingImageOmitsDimensionsWithoutFailing(t *testing.T) {
	html := renderPost(t, "![a](./images/missing.png)\n\n![b](./images/bad.png)", map[string][]byte{
		"images/bad.png": []byte("this is not a png"),
	})
	mustNotContain(t, html, "width=")
	mustContain(t, html, `src="/blog/demo/images/missing.png"`)
	mustContain(t, html, `src="/blog/demo/images/bad.png"`)
}

// Loading hints belong to buildcmd's injectDefaultLazyLoading, which works at
// page scope and exempts the page's first <img> as the LCP candidate. It
// respects any loading value already present, so emitting one here would
// override that exemption — and a post whose first media is a video would then
// stamp loading="lazy" on the page's actual LCP image.
func TestRendererEmitsNoLoadingHintsAndLeavesThemToTheTransform(t *testing.T) {
	html := renderPost(t, "![a](./images/a.png)\n\n![b](./images/b.png)", map[string][]byte{
		"images/a.png": encodePNG(t, 10, 10),
		"images/b.png": encodePNG(t, 20, 20),
	})
	mustNotContain(t, html, "loading=")
	mustNotContain(t, html, "decoding=")
	mustNotContain(t, html, "fetchpriority=")
}

// The same holds when a video precedes the page's first image: no loading
// attribute may be emitted for that image, or the LCP exemption is lost.
func TestVideoBeforeImageDoesNotMarkThatImageLazy(t *testing.T) {
	html := renderPost(t, "![clip](./videos/clip.mp4)\n\n![hero](./images/a.png)", map[string][]byte{
		"videos/clip.mp4": []byte("fake"),
		"images/a.png":    encodePNG(t, 1600, 900),
	})
	mustNotContain(t, html, "loading=")
	mustContain(t, html, `width="1600" height="900"`)
}

func TestImageWithoutAltStillEmitsEmptyAlt(t *testing.T) {
	html := renderPost(t, "![](./images/a.png)", map[string][]byte{
		"images/a.png": encodePNG(t, 10, 10),
	})
	// alt="" is valid markup for a decorative image and is what the repo's
	// alt-text validator accepts; a fully absent alt is what it warns on.
	mustContain(t, html, `alt=""`)
}

// --- captions -------------------------------------------------------------

// <figure> is flow content: inside <p> every browser silently closes the
// paragraph. The position assertions matter more than the presence one here.
func TestCaptionedBlockImageEmitsFigureOutsideAnyParagraph(t *testing.T) {
	html := renderPost(t, `![a](./images/a.png "The caption")`, map[string][]byte{
		"images/a.png": encodePNG(t, 10, 10),
	})
	mustContain(t, html, "<figure><img")
	mustContain(t, html, "<figcaption>The caption</figcaption></figure>")
	mustNotContain(t, html, "<p><figure")
	mustNotContain(t, html, "<figure></p>")
	if strings.Contains(html, "<p>") {
		t.Errorf("a sole captioned image should emit no paragraph at all\ngot:\n%s", html)
	}
}

// An image among text cannot become a figure, so its title degrades to a
// title attribute rather than being dropped.
func TestInlineImageWithTitleStaysInlineWithTitleAttribute(t *testing.T) {
	html := renderPost(t, `Text ![a](./images/a.png "Tooltip") more text.`, map[string][]byte{
		"images/a.png": encodePNG(t, 10, 10),
	})
	mustContain(t, html, `title="Tooltip"`)
	mustNotContain(t, html, "<figure")
	mustNotContain(t, html, "<figcaption")
	mustContain(t, html, "<p>Text <img")
}

func TestCaptionIsHTMLEscaped(t *testing.T) {
	html := renderPost(t, "![a](./images/a.png \"Tom & <b>Jerry</b>\")", map[string][]byte{
		"images/a.png": encodePNG(t, 10, 10),
	})
	mustContain(t, html, "Tom &amp; &lt;b&gt;Jerry&lt;/b&gt;")
	mustNotContain(t, html, "<b>Jerry")
}

func TestCaptionedVideoAlsoGetsFigure(t *testing.T) {
	html := renderPost(t, `![a clip](./videos/clip.mp4 "Clip caption")`, map[string][]byte{
		"videos/clip.mp4": []byte("fake"),
	})
	mustContain(t, html, "<figure><video")
	mustContain(t, html, "<figcaption>Clip caption</figcaption></figure>")
	mustNotContain(t, html, "<p><figure")
}

// --- destinations ---------------------------------------------------------

func TestOffSiteAndAbsoluteDestinationsAreNotRewritten(t *testing.T) {
	html := renderPost(t,
		"![a](https://example.com/x.png)\n\n![b](//cdn.example.com/y.png)\n\n![c](/already/z.png)",
		nil)
	mustContain(t, html, `src="https://example.com/x.png"`)
	mustContain(t, html, `src="//cdn.example.com/y.png"`)
	mustContain(t, html, `src="/already/z.png"`)
	mustNotContain(t, html, "/blog/demo/https")
	mustNotContain(t, html, "/blog/demo/already")
}

// A hostile scheme must reach goldmark's IsDangerousURL guard rather than
// being defused by accident into a relative path that hides it.
func TestDangerousSchemeIsBlankedNotRewritten(t *testing.T) {
	html := renderPost(t, "![x](javascript:alert&#40;1&#41;)", nil)
	mustContain(t, html, `src=""`)
	mustNotContain(t, html, "javascript:")
}

func TestPathTraversalDoesNotReadOutsideThePostDirectory(t *testing.T) {
	html := renderPost(t, "![a](./../../etc/passwd)", nil)
	// The path is still rewritten under the post's URL space, but no
	// dimensions are read for it — localPath refuses traversal.
	mustNotContain(t, html, "width=")
}

// --- frontmatter thumbnail ------------------------------------------------

func TestThumbnailResolutionMatchesBodyRules(t *testing.T) {
	root := t.TempDir()
	postDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(postDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\ntitle: Demo\ndate: \"2026-01-01\"\n" +
		"thumbnail: \"https://cdn.example.com/cover.webp\"\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(postDir, "index.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPostsDir(root)
	if err != nil {
		t.Fatal(err)
	}
	// Previously this produced /blog/demo/https://cdn.example.com/cover.webp.
	if got := loaded[0].Meta.Thumbnail; got != "https://cdn.example.com/cover.webp" {
		t.Errorf("off-site thumbnail was rewritten: got %q", got)
	}
}

// --- media copying --------------------------------------------------------

func TestCopyPostMediaCopiesImagesAndVideos(t *testing.T) {
	root := t.TempDir()
	postDir := filepath.Join(root, "demo")
	for _, sub := range []string{"images", "videos", "drafts"} {
		if err := os.MkdirAll(filepath.Join(postDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for rel, data := range map[string][]byte{
		"images/a.png":     encodePNG(t, 10, 10),
		"videos/clip.mp4":  []byte("fake"),
		"drafts/secret.md": []byte("unpublished"),
	} {
		if err := os.WriteFile(filepath.Join(postDir, filepath.FromSlash(rel)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := t.TempDir()
	posts := []Post{{Meta: PostMeta{Slug: "demo"}, Dir: postDir}}
	if err := CopyPostMedia(posts, out); err != nil {
		t.Fatalf("CopyPostMedia: %v", err)
	}
	for _, rel := range []string{"blog/demo/images/a.png", "blog/demo/videos/clip.mp4"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s to be copied: %v", rel, err)
		}
	}
	// Only the declared media subdirectories are published.
	if _, err := os.Stat(filepath.Join(out, "blog", "demo", "drafts")); err == nil {
		t.Error("drafts/ must not be copied into the build output")
	}
}

func TestCopyPostMediaSkipsPostWithNoMediaDirs(t *testing.T) {
	root := t.TempDir()
	postDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(postDir, 0o755); err != nil {
		t.Fatal(err)
	}
	posts := []Post{{Meta: PostMeta{Slug: "demo"}, Dir: postDir}}
	if err := CopyPostMedia(posts, t.TempDir()); err != nil {
		t.Errorf("a post with no media dirs must not fail the build: %v", err)
	}
}
