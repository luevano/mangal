package web

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/web/api"
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

// GetImage implements api.StrictServerInterface.
func (s *Server) GetImage(ctx context.Context, request api.GetImageRequestObject) (api.GetImageResponseObject, error) {
	if err := checkHTTPURL(request.Params.Url); err != nil {
		return nil, fmt.Errorf("url: %w", err)
	}
	referer := ""
	if request.Params.Referer != nil && *request.Params.Referer != "" {
		referer = *request.Params.Referer
		if err := checkHTTPURL(referer); err != nil {
			return nil, fmt.Errorf("referer: %w", err)
		}
	}

	img, ok := s.images.get(request.Params.Url)
	if !ok {
		var err error
		img, err = s.fetchImage(ctx, request.Params.Url, referer)
		if err != nil {
			return nil, err
		}
		s.images.add(request.Params.Url, img)
	}

	return api.GetImage200ImageResponse{
		Body:          bytes.NewReader(img.data),
		ContentType:   img.contentType,
		ContentLength: int64(len(img.data)),
	}, nil
}

func (s *Server) fetchImage(ctx context.Context, rawURL, referer string) (cachedImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return cachedImage{}, fmt.Errorf("get image: %w", err)
	}
	req.Header.Set("User-Agent", config.Download.UserAgent.Get())
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return cachedImage{}, fmt.Errorf("get image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cachedImage{}, fmt.Errorf("get image: %s: %w", resp.Status, errUpstream)
	}
	if resp.ContentLength > maxImageSize {
		return cachedImage{}, fmt.Errorf("get image: %d bytes, more than %d: %w", resp.ContentLength, maxImageSize, errUpstream)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return cachedImage{}, fmt.Errorf("get image: %w", err)
	}
	if len(data) > maxImageSize {
		return cachedImage{}, fmt.Errorf("get image: more than %d bytes: %w", maxImageSize, errUpstream)
	}

	contentType, err := imageType(resp.Header.Get("Content-Type"), data)
	if err != nil {
		return cachedImage{}, fmt.Errorf("get image: %w", err)
	}
	return cachedImage{data: data, contentType: contentType}, nil
}

// rasterTypes are the image types the proxy passes: no SVG, which runs scripts when opened
// from mangal's origin.
var rasterTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/avif": true,
	"image/bmp":  true,
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
	return "", fmt.Errorf("not an image (%q): %w", header, errUpstream)
}

// imageHeaders keep a proxied image from being anything but an image in the browser.
func imageHeaders(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			h := c.Response().Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
		}
		return next(c)
	}
}

func checkHTTPURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", errBadRequest, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q isn't an http(s) url: %w", rawURL, errBadRequest)
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
