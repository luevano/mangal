package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// iconRetry is how long a site without an icon isn't asked again.
const iconRetry = 24 * time.Hour

// Icon is a site's icon: icon if not empty and an image, else the website's /favicon.ico
// (fetched as Image's are), kept in the icon dir and served from there afterwards. Icons
// are fetched by each server, never shipped with mangal. A site without one is
// ErrNotFound, and isn't asked again for iconRetry.
func (s *Server) Icon(ctx context.Context, icon, website string) ([]byte, string, error) {
	var urls []string
	if icon != "" {
		urls = append(urls, icon)
	}
	if u, err := url.Parse(website); err == nil && u.Host != "" {
		urls = append(urls, u.ResolveReference(&url.URL{Path: "/favicon.ico"}).String())
	}
	if len(urls) == 0 {
		return nil, "", fmt.Errorf("no icon or website: %w", ErrNotFound)
	}
	sum := sha256.Sum256([]byte(strings.Join(urls, "\n")))
	key := hex.EncodeToString(sum[:16])
	v, err, _ := s.icons.Do(key, func() (any, error) { return s.icon(ctx, key, urls) })
	if err != nil {
		return nil, "", err
	}
	img := v.(cachedImage)
	return img.data, img.contentType, nil
}

// icon is the icon from the dir, else fetched and stored as <key>.<subtype>; a failure is
// remembered as <key>.failed (its mtime).
func (s *Server) icon(ctx context.Context, key string, urls []string) (cachedImage, error) {
	if files, _ := filepath.Glob(filepath.Join(s.iconDir, key+".*")); len(files) == 1 {
		ext := strings.TrimPrefix(filepath.Base(files[0]), key+".")
		if rasterTypes["image/"+ext] {
			if data, err := os.ReadFile(files[0]); err == nil {
				return cachedImage{data: data, contentType: "image/" + ext}, nil
			}
		} else if ext == "failed" {
			if fi, err := os.Stat(files[0]); err == nil && time.Since(fi.ModTime()) < iconRetry {
				return cachedImage{}, fmt.Errorf("no icon (asked %s): %w", fi.ModTime().Format(time.DateTime), ErrNotFound)
			}
		}
	}

	var errs []string
	for _, u := range urls {
		if err := checkHTTPURL(u); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		img, err := s.fetchImage(ctx, u, "")
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		if err := s.storeIcon(key, strings.TrimPrefix(img.contentType, "image/"), img.data); err != nil {
			return cachedImage{}, err
		}
		return img, nil
	}
	if ctx.Err() == nil {
		if err := s.storeIcon(key, "failed", []byte(strings.Join(errs, "\n"))); err != nil {
			return cachedImage{}, err
		}
	}
	return cachedImage{}, fmt.Errorf("no icon (%s): %w", strings.Join(errs, "; "), ErrNotFound)
}

// storeIcon replaces the key's file with <key>.<ext>.
func (s *Server) storeIcon(key, ext string, data []byte) error {
	if err := os.MkdirAll(s.iconDir, 0o755); err != nil {
		return fmt.Errorf("store icon: %w", err)
	}
	old, _ := filepath.Glob(filepath.Join(s.iconDir, key+".*"))
	tmp, err := os.CreateTemp(s.iconDir, ".tmp-"+key)
	if err != nil {
		return fmt.Errorf("store icon: %w", err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	name := filepath.Join(s.iconDir, key+"."+ext)
	if err == nil {
		err = os.Rename(tmp.Name(), name)
	}
	if err != nil {
		return fmt.Errorf("store icon: %w", err)
	}
	for _, f := range old {
		if f != name {
			_ = os.Remove(f)
		}
	}
	return nil
}
