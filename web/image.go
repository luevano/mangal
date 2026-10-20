package web

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/labstack/echo/v5"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/util/netguard"
)

const (
	// maxImageSize is the largest image the proxy passes through.
	maxImageSize = 20 << 20
	// imageCacheSize is how many bytes of images the proxy keeps in memory.
	imageCacheSize = 128 << 20
)

type cachedImage struct {
	data        []byte
	contentType string
}

// Image is the image at the url (fetched with the referer, or from the cache) and its
// type: only from public addresses, only raster images.
func (s *Server) Image(ctx context.Context, rawURL, referer string) ([]byte, string, error) {
	if err := checkHTTPURL(rawURL); err != nil {
		return nil, "", fmt.Errorf("url: %w", err)
	}
	if referer != "" {
		if err := checkHTTPURL(referer); err != nil {
			return nil, "", fmt.Errorf("referer: %w", err)
		}
	}
	img, ok := s.images.get(rawURL)
	if !ok {
		var err error
		img, err = s.fetchImage(ctx, rawURL, referer, imageType)
		if err != nil {
			return nil, "", err
		}
		s.images.add(rawURL, img)
	}
	return img.data, img.contentType, nil
}

// temporaryError is a fetch that may work later: no answer, a 5xx or a 429.
type temporaryError struct{ error }

func (e temporaryError) Unwrap() error { return e.error }

// fetchImage gets the url, typed by typeOf from its Content-Type and content.
func (s *Server) fetchImage(ctx context.Context, rawURL, referer string, typeOf func(string, []byte) (string, error)) (cachedImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return cachedImage{}, fmt.Errorf("get image: %w", err)
	}
	req.Header.Set("User-Agent", config.Download.UserAgent.Get())
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := s.httpClient.Do(req)
	if errors.Is(err, netguard.ErrRefused) {
		return cachedImage{}, fmt.Errorf("get image: %w: %w", err, ErrBadRequest)
	}
	if err != nil {
		return cachedImage{}, temporaryError{fmt.Errorf("get image: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("get image: %s: %w", resp.Status, ErrUpstream)
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			err = temporaryError{err}
		}
		return cachedImage{}, err
	}
	if resp.ContentLength > maxImageSize {
		return cachedImage{}, fmt.Errorf("get image: %d bytes, more than %d: %w", resp.ContentLength, maxImageSize, ErrUpstream)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return cachedImage{}, temporaryError{fmt.Errorf("get image: %w", err)}
	}
	if len(data) > maxImageSize {
		return cachedImage{}, fmt.Errorf("get image: more than %d bytes: %w", maxImageSize, ErrUpstream)
	}

	contentType, err := typeOf(resp.Header.Get("Content-Type"), data)
	if err != nil {
		return cachedImage{}, fmt.Errorf("get image: %w", err)
	}
	return cachedImage{data: data, contentType: contentType}, nil
}

// rasterTypes are the image types the proxy passes: no SVG, which runs scripts when opened
// from mangal's origin (icons may be SVG, see iconType).
var rasterTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/avif": true,
	"image/bmp":  true,
	// favicons
	"image/x-icon":             true,
	"image/vnd.microsoft.icon": true,
}

// imageType is the image's media type, from the header or else the content.
func imageType(header string, data []byte) (string, error) {
	if mediaType, _, err := mime.ParseMediaType(header); err == nil && rasterTypes[mediaType] {
		return mediaType, nil
	}
	// some sites send images as application/octet-stream
	if sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(data)); rasterTypes[sniffed] {
		return sniffed, nil
	}
	return "", fmt.Errorf("not an image (%q): %w", header, ErrUpstream)
}

// ImageHeaders keep a proxied image (any /api/ answer) from being anything but an image
// in the browser: an SVG icon opened directly gets no scripts or requests, only its own
// styles (raw.githubusercontent.com's policy).
func ImageHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			h := c.Response().Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		}
		return next(c)
	}
}

func checkHTTPURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q isn't an http(s) url: %w", rawURL, ErrBadRequest)
	}
	return nil
}

// imageCache is an LRU cache of images, up to size bytes.
type imageCache struct {
	mu    sync.Mutex
	size  int
	used  int
	order *list.List // of *imageEntry, most recent first
	byURL map[string]*list.Element
}

type imageEntry struct {
	url   string
	image cachedImage
}

func newImageCache(size int) *imageCache {
	return &imageCache{size: size, order: list.New(), byURL: map[string]*list.Element{}}
}

func (c *imageCache) get(url string) (cachedImage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.byURL[url]
	if !ok {
		return cachedImage{}, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*imageEntry).image, true
}

func (c *imageCache) add(url string, img cachedImage) {
	if len(img.data) > c.size {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.byURL[url]; ok {
		c.used -= len(e.Value.(*imageEntry).image.data)
		c.order.Remove(e)
		delete(c.byURL, url)
	}
	for c.used+len(img.data) > c.size {
		oldest := c.order.Back()
		entry := oldest.Value.(*imageEntry)
		c.used -= len(entry.image.data)
		c.order.Remove(oldest)
		delete(c.byURL, entry.url)
	}
	c.byURL[url] = c.order.PushFront(&imageEntry{url: url, image: img})
	c.used += len(img.data)
}

// UncachedImage is CachedImage without a cache.
func UncachedImage(_ string, fetch func() ([]byte, error)) ([]byte, string, error) {
	data, err := fetch()
	if err != nil {
		return nil, "", err
	}
	contentType, err := imageType("", data)
	return data, contentType, err
}

// CachedImage is the image under key from the cache, else fetched (and cached) with
// fetch: only raster images, like Image's.
func (s *Server) CachedImage(key string, fetch func() ([]byte, error)) ([]byte, string, error) {
	if img, ok := s.images.get(key); ok {
		return img.data, img.contentType, nil
	}
	data, err := fetch()
	if err != nil {
		return nil, "", err
	}
	contentType, err := imageType("", data)
	if err != nil {
		return nil, "", err
	}
	s.images.add(key, cachedImage{data: data, contentType: contentType})
	return data, contentType, nil
}
