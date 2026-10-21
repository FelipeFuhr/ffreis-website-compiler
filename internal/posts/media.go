package posts

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"ffreis-website-compiler/internal/mediameta"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// mediaRenderer replaces goldmark's default ast.KindImage rendering so that a
// post's media gets the attributes a static site actually needs.
//
// Markdown has exactly one media syntax — `![alt](dest "title")` — and
// goldmark renders every one of them as a bare <img>. Three consequences this
// renderer fixes:
//
//  1. `![demo](./videos/demo.mp4)` emitted an <img> pointing at a video, which
//     a browser renders as a broken image. A video destination now renders a
//     real <video> element.
//  2. No intrinsic width/height meant every post image contributed layout
//     shift. Dimensions are now read off the file on disk when it is decodable.
//
// Deliberately NOT handled here: loading/decoding hints. buildcmd's
// injectDefaultLazyLoading already owns them, at page scope — which is the
// correct scope, because a post page's template can place images above the
// post body, so "first image in the post" is not "first image on the page".
// It also defers to any loading attribute already present, so emitting one
// here would override its LCP exemption and could ship a lazy LCP image —
// the very anti-pattern that transform exists to prevent.
//
// The renderer is constructed per post because it carries that post's slug and
// directory. goldmark instances are cheap relative to reading and rendering a
// post file, and per-post construction avoids sharing render state across
// documents.
type mediaRenderer struct {
	html.Config

	// slug is the post directory name, used to build root-relative URLs.
	slug string
	// postDir is the absolute path to the post directory, used to stat media
	// and read image headers.
	postDir string
}

func newMediaRenderer(slug, postDir string, opts ...html.Option) renderer.NodeRenderer {
	r := &mediaRenderer{Config: html.NewConfig(), slug: slug, postDir: postDir}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

func (r *mediaRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, r.renderMedia)
	reg.Register(ast.KindParagraph, r.renderParagraph)
}

// renderParagraph suppresses the <p> wrapper when the paragraph exists only to
// hold a captioned image. <figure> is flow content and is invalid inside <p>:
// emitting one there silently closes the paragraph in every browser. Markdown
// gives no block-image syntax, so "the image is the whole paragraph" is the
// only signal available for block context.
func (r *mediaRenderer) renderParagraph(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if figureImage(node) != nil {
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = w.WriteString("<p>")
	} else {
		_, _ = w.WriteString("</p>\n")
	}
	return ast.WalkContinue, nil
}

// figureImage reports the image a paragraph should render as a <figure>: its
// sole child, carrying a title to use as the caption. Any other shape — an
// image among text, several images, no title — is inline and stays an <img>.
func figureImage(node ast.Node) *ast.Image {
	if node.Kind() != ast.KindParagraph || node.ChildCount() != 1 {
		return nil
	}
	img, ok := node.FirstChild().(*ast.Image)
	if !ok || len(img.Title) == 0 {
		return nil
	}
	return img
}

// mediaExtension installs mediaRenderer ahead of goldmark's default HTML
// renderer. Priority 100 beats the default renderer's 1000 (lower wins), so
// only ast.KindImage is taken over and every other node still renders
// normally.
type mediaExtension struct {
	slug    string
	postDir string
}

func (e mediaExtension) Extend(m goldmark.Markdown) {
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(newMediaRenderer(e.slug, e.postDir), 100),
	))
}

// renderMedia emits the element for one markdown image node.
func (r *mediaRenderer) renderMedia(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	n, ok := node.(*ast.Image)
	if !ok {
		// Registered only for ast.KindImage, so this is unreachable; handled
		// rather than asserted so a future registration cannot panic.
		return ast.WalkContinue, nil
	}

	// Destination is rewritten to a root-relative site path so the emitted
	// HTML needs no further post-processing.
	dest := r.resolveDest(string(n.Destination))

	alt := string(nodeText(n, source))

	// A caption is only emitted where <figure> is legal: a paragraph whose
	// sole content is this image. Elsewhere the markdown title degrades to a
	// title attribute, which is what goldmark's default renderer does too.
	caption := ""
	if parent := n.Parent(); parent != nil && figureImage(parent) == n {
		caption = string(n.Title)
	}

	if caption != "" {
		_, _ = w.WriteString("<figure>")
	}

	switch mediaKind(string(n.Destination)) {
	case mediameta.KindVideo:
		r.writeVideo(w, dest, alt, titleAttr(n, caption))
	default:
		r.writeImage(w, dest, alt, titleAttr(n, caption))
	}

	if caption != "" {
		_, _ = w.WriteString("<figcaption>")
		_, _ = w.Write(util.EscapeHTML([]byte(caption)))
		_, _ = w.WriteString("</figcaption></figure>\n")
	}

	return ast.WalkSkipChildren, nil
}

// titleAttr returns the markdown title to put in a title attribute: present
// only when it is not already being shown as a visible caption.
func titleAttr(n *ast.Image, caption string) string {
	if caption != "" {
		return ""
	}
	return string(n.Title)
}

func (r *mediaRenderer) writeImage(w util.BufWriter, dest, alt, title string) {
	_, _ = w.WriteString(`<img src="`)
	r.writeURL(w, dest)
	_, _ = w.WriteString(`" alt="`)
	_, _ = w.Write(util.EscapeHTML([]byte(alt)))
	_, _ = w.WriteString(`"`)
	writeOptionalAttr(w, "title", title)

	if width, height, ok := r.dimensions(dest); ok {
		writeDimensions(w, width, height)
	}

	// No loading/decoding attribute: buildcmd's injectDefaultLazyLoading adds
	// them page-wide and exempts the page's first <img> as the LCP candidate.
	// It respects any loading value already present, so writing one here
	// would silently disable that exemption.
	_, _ = w.WriteString(">")
}

func (r *mediaRenderer) writeVideo(w util.BufWriter, dest, alt, title string) {
	_, _ = w.WriteString(`<video src="`)
	r.writeURL(w, dest)
	_, _ = w.WriteString(`"`)

	// A poster frame beside the video both gives the element something to
	// paint before play and supplies the dimensions that stop layout shift —
	// mp4/webm headers are not parsed here.
	if poster, ok := r.findPoster(dest); ok {
		_, _ = w.WriteString(` poster="`)
		r.writeURL(w, poster)
		_, _ = w.WriteString(`"`)
		if width, height, dok := r.dimensions(poster); dok {
			writeDimensions(w, width, height)
		}
	}

	// <video> has no alt attribute; the accessible name comes from aria-label.
	writeOptionalAttr(w, "aria-label", alt)
	writeOptionalAttr(w, "title", title)

	// controls: unplayable without them, since markdown carries no UI hints.
	// preload=metadata: enough for duration and the first frame, without
	// pulling the whole file on page load.
	// playsinline: iOS Safari otherwise takes over the screen on play.
	_, _ = w.WriteString(` controls preload="metadata" playsinline>`)

	// Fallback content, shown only by a browser that cannot render <video> at
	// all. Deliberately plain text rather than an <a href> download link:
	// internal/linkcheck validates every <a href> against the set of
	// GENERATED PAGES, so an anchor pointing at an .mp4 is reported as a
	// broken internal link even though the file is right there in the output.
	// A media download affordance belongs in a site template that can style
	// and label it, not silently inside every video a post embeds.
	_, _ = w.Write(util.EscapeHTML([]byte(fallbackText(alt))))
	_, _ = w.WriteString(`</video>`)
}

// writeOptionalAttr writes ` name="value"` when value is non-empty.
func writeOptionalAttr(w util.BufWriter, name, value string) {
	if value == "" {
		return
	}
	_, _ = w.WriteString(" ")
	_, _ = w.WriteString(name)
	_, _ = w.WriteString(`="`)
	_, _ = w.Write(util.EscapeHTML([]byte(value)))
	_, _ = w.WriteString(`"`)
}

func writeDimensions(w util.BufWriter, width, height int) {
	_, _ = w.WriteString(` width="`)
	_, _ = w.WriteString(strconv.Itoa(width))
	_, _ = w.WriteString(`" height="`)
	_, _ = w.WriteString(strconv.Itoa(height))
	_, _ = w.WriteString(`"`)
}

// fallbackText names the video for a browser that cannot play it, preferring
// the author's alt text over a generic string.
func fallbackText(alt string) string {
	if alt != "" {
		return alt + " (video not supported by this browser)"
	}
	return "This browser cannot play the embedded video."
}

// writeURL applies the same escaping and dangerous-scheme guard goldmark's own
// image renderer uses, so overriding the renderer does not weaken it.
func (r *mediaRenderer) writeURL(w util.BufWriter, dest string) {
	if r.Unsafe || !html.IsDangerousURL([]byte(dest)) {
		_, _ = w.Write(util.EscapeHTML(util.URLEscape([]byte(dest), true)))
	}
}

func (r *mediaRenderer) resolveDest(dest string) string {
	return ResolvePostPath(dest, r.slug)
}

// ResolvePostPath turns a post-relative media path into a root-relative site
// path under /blog/<slug>/. Absolute paths, protocol-relative URLs and
// anything carrying a URI scheme are left alone — those already name their own
// location. Shared by the body renderer and the frontmatter thumbnail so one
// set of rules governs both.
func ResolvePostPath(dest, slug string) string {
	if dest == "" || strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "#") {
		return dest
	}
	// Anything carrying a URI scheme already names its own location and must
	// pass through untouched — including a hostile one like `javascript:`,
	// which writeURL's IsDangerousURL guard is what strips. Prefixing it into
	// a relative path instead would defuse this one case by accident while
	// hiding it from the guard that exists to handle it.
	if strings.HasPrefix(dest, "//") || hasURIScheme(dest) {
		return dest
	}
	clean := strings.TrimPrefix(dest, "./")
	return "/blog/" + slug + "/" + clean
}

// hasURIScheme reports whether dest begins with a RFC 3986 scheme followed by
// a colon (`https:`, `mailto:`, `javascript:`). A bare Windows-style drive or a
// path containing a later colon is not a scheme and is not matched.
func hasURIScheme(dest string) bool {
	for i := 0; i < len(dest); i++ {
		c := dest[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			continue
		case c >= '0' && c <= '9', c == '+', c == '-', c == '.':
			if i == 0 {
				return false // a scheme must start with a letter
			}
		case c == ':':
			return i > 0
		default:
			return false
		}
	}
	return false
}

// localPath maps a rewritten root-relative URL back to the file on disk, or
// returns false when the URL does not point inside this post's directory.
func (r *mediaRenderer) localPath(dest string) (string, bool) {
	prefix := "/blog/" + r.slug + "/"
	if r.postDir == "" || !strings.HasPrefix(dest, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(dest, prefix)
	// Reject traversal: media must resolve inside the post directory.
	if rel == "" || strings.Contains(rel, "..") {
		return "", false
	}
	return filepath.Join(r.postDir, filepath.FromSlash(rel)), true
}

// dimensions reads intrinsic size off the file on disk. A missing or
// undecodable file is not an error: the attributes are simply omitted, which
// is the pre-existing behaviour rather than a build failure.
func (r *mediaRenderer) dimensions(dest string) (width, height int, ok bool) {
	local, found := r.localPath(dest)
	if !found {
		return 0, 0, false
	}
	// localPath has already confined this to postDir and rejected traversal.
	data, err := os.ReadFile(local)
	if err != nil {
		return 0, 0, false
	}
	w, h, err := mediameta.ImageDimensions(data)
	if err != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// findPoster looks for a poster frame sharing the video's basename.
func (r *mediaRenderer) findPoster(dest string) (string, bool) {
	for _, candidate := range mediameta.PosterCandidates(dest) {
		local, found := r.localPath(candidate)
		if !found {
			continue
		}
		if info, err := os.Stat(local); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func mediaKind(dest string) mediameta.Kind {
	return mediameta.KindForPath(path.Base(dest))
}

// nodeText collects the plain-text content of an inline node's subtree, which
// for an image node is its alt text.
func nodeText(n ast.Node, source []byte) []byte {
	var out []byte
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			out = append(out, t.Segment.Value(source)...)
		case *ast.String:
			out = append(out, t.Value...)
		default:
			out = append(out, nodeText(c, source)...)
		}
	}
	return out
}
