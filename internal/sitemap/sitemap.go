package sitemap

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type Config struct {
	BaseURL        string    `yaml:"base_url"`
	DefaultLastmod string    `yaml:"default_lastmod"`
	URLs           []URLItem `yaml:"urls"`
}

type URLItem struct {
	Path        string `yaml:"path"`
	Lastmod     string `yaml:"lastmod"`
	LastmodFrom string `yaml:"lastmod_from"`
	Changefreq  string `yaml:"changefreq"`
	Priority    string `yaml:"priority"`
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	Xmlns   string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

type urlEntry struct {
	Loc        string `xml:"loc"`
	Lastmod    string `xml:"lastmod,omitempty"`
	Changefreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading sitemap config: %w", err)
	}
	defer f.Close()

	cfg, err := parseConfigYAML(f)
	if err != nil {
		return Config{}, fmt.Errorf("parsing sitemap config yaml: %w", err)
	}

	if strings.TrimSpace(cfg.BaseURL) == "" {
		return Config{}, fmt.Errorf("sitemap config: base_url is required")
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")

	if len(cfg.URLs) == 0 {
		return Config{}, fmt.Errorf("sitemap config: urls must contain at least one entry")
	}

	if cfg.DefaultLastmod != "" && !dateRE.MatchString(cfg.DefaultLastmod) {
		return Config{}, fmt.Errorf("sitemap config: default_lastmod must be YYYY-MM-DD")
	}

	return cfg, nil
}

func parseConfigYAML(f *os.File) (Config, error) {
	scanner := bufio.NewScanner(f)
	p := &configYAMLParser{}

	for scanner.Scan() {
		p.lineNo++
		trimmed := strings.TrimSpace(scanner.Text())
		if isIgnorableYAMLLine(trimmed) {
			continue
		}
		if err := p.parseLine(trimmed); err != nil {
			return Config{}, err
		}
	}

	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	p.flushCurrent()

	return p.cfg, nil
}

type configYAMLParser struct {
	cfg     Config
	lineNo  int
	inURLs  bool
	current *URLItem
}

func isIgnorableYAMLLine(trimmed string) bool {
	return trimmed == "" || strings.HasPrefix(trimmed, "#")
}

func (p *configYAMLParser) flushCurrent() {
	if p.current == nil {
		return
	}
	p.cfg.URLs = append(p.cfg.URLs, *p.current)
	p.current = nil
}

func (p *configYAMLParser) parseLine(trimmed string) error {
	if trimmed == "urls:" {
		p.inURLs = true
		return nil
	}
	if !p.inURLs {
		return p.parseTopLevel(trimmed)
	}
	return p.parseURLSectionLine(trimmed)
}

func (p *configYAMLParser) parseTopLevel(trimmed string) error {
	key, val, ok := parseKeyValue(trimmed)
	if !ok {
		return fmt.Errorf("line %d: expected key: value", p.lineNo)
	}

	switch key {
	case "base_url":
		p.cfg.BaseURL = unquoteYAML(val)
	case "default_lastmod":
		p.cfg.DefaultLastmod = unquoteYAML(val)
	default:
		return fmt.Errorf("line %d: unsupported key %q", p.lineNo, key)
	}
	return nil
}

func (p *configYAMLParser) parseURLSectionLine(trimmed string) error {
	if strings.HasPrefix(trimmed, "- ") {
		return p.startNewURLItem(trimmed)
	}

	if p.current == nil {
		return fmt.Errorf("line %d: url field without '-' entry", p.lineNo)
	}

	key, val, ok := parseKeyValue(trimmed)
	if !ok {
		return fmt.Errorf("line %d: expected key: value in url item", p.lineNo)
	}
	return assignURLField(p.current, key, val, p.lineNo)
}

func (p *configYAMLParser) startNewURLItem(trimmed string) error {
	p.flushCurrent()
	p.current = &URLItem{}

	itemKV := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	if itemKV == "" {
		return nil
	}

	key, val, ok := parseKeyValue(itemKV)
	if !ok {
		return fmt.Errorf("line %d: expected - key: value", p.lineNo)
	}
	return assignURLField(p.current, key, val, p.lineNo)
}

func parseKeyValue(s string) (string, string, bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

func unquoteYAML(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func assignURLField(u *URLItem, key, value string, lineNo int) error {
	val := unquoteYAML(value)
	switch key {
	case "path":
		u.Path = val
	case "lastmod":
		u.Lastmod = val
	case "lastmod_from":
		u.LastmodFrom = val
	case "changefreq":
		u.Changefreq = val
	case "priority":
		u.Priority = val
	default:
		return fmt.Errorf("line %d: unsupported url key %q", lineNo, key)
	}
	return nil
}

func GenerateXML(cfg Config, websiteRoot string) ([]byte, error) {
	entries := make([]urlEntry, 0, len(cfg.URLs))
	for _, u := range cfg.URLs {
		path := strings.TrimSpace(u.Path)
		if path == "" {
			return nil, fmt.Errorf("sitemap config: url path cannot be empty")
		}

		loc := buildLoc(cfg.BaseURL, path)

		lastmod, err := resolveLastmod(cfg, u, websiteRoot)
		if err != nil {
			return nil, err
		}

		entries = append(entries, urlEntry{
			Loc:        loc,
			Lastmod:    lastmod,
			Changefreq: strings.TrimSpace(u.Changefreq),
			Priority:   strings.TrimSpace(u.Priority),
		})
	}

	doc := urlSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  entries,
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("building sitemap xml: %w", err)
	}

	return append([]byte(xml.Header), out...), nil
}

func buildLoc(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if path == "/" {
		return baseURL + "/"
	}
	if strings.HasPrefix(path, "/") {
		return baseURL + path
	}
	return baseURL + "/" + path
}

func resolveLastmod(cfg Config, u URLItem, websiteRoot string) (string, error) {
	lastmod := strings.TrimSpace(u.Lastmod)
	if lastmod != "" {
		if !dateRE.MatchString(lastmod) {
			return "", fmt.Errorf("sitemap config: lastmod for path %q must be YYYY-MM-DD", u.Path)
		}
		return lastmod, nil
	}

	lastmodFrom := strings.TrimSpace(u.LastmodFrom)
	if lastmodFrom != "" {
		sourcePath := lastmodFrom
		if !filepath.IsAbs(sourcePath) {
			sourcePath = filepath.Join(websiteRoot, sourcePath)
		}
		info, err := os.Stat(sourcePath)
		if err != nil {
			return "", fmt.Errorf("sitemap config: lastmod_from not found for path %q: %w", u.Path, err)
		}
		return info.ModTime().In(time.UTC).Format("2006-01-02"), nil
	}

	if cfg.DefaultLastmod != "" {
		return cfg.DefaultLastmod, nil
	}

	return "", nil
}
