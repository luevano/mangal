package pages

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/html"
)

// epubReadMax caps what's read of an epub's container, package and page documents.
const epubReadMax = 8 << 20

// epubSpine is the images an epub shows in reading order: its spine's items, an image or
// the images of a document. Nil when it can't tell (not a proper epub): any image of the
// file by name then.
func epubSpine(r *zip.Reader) []string {
	var container struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if !readEpubXML(r, "META-INF/container.xml", &container) || len(container.Rootfiles) == 0 {
		return nil
	}
	opfPath := container.Rootfiles[0].Path
	var opf struct {
		Items []struct {
			ID   string `xml:"id,attr"`
			Href string `xml:"href,attr"`
		} `xml:"manifest>item"`
		Spine []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
	}
	if !readEpubXML(r, opfPath, &opf) {
		return nil
	}
	hrefs := make(map[string]string, len(opf.Items))
	for _, item := range opf.Items {
		hrefs[item.ID] = item.Href
	}
	var images []string
	for _, ref := range opf.Spine {
		item, ok := epubRef(opfPath, hrefs[ref.IDRef])
		switch {
		case !ok:
		case isImage(item):
			images = append(images, item)
		default:
			images = append(images, epubDocImages(r, item)...)
		}
	}
	return images
}

func readEpubXML(r *zip.Reader, name string, v any) bool {
	f, err := r.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	return xml.NewDecoder(io.LimitReader(f, epubReadMax)).Decode(v) == nil
}

// epubRef is the file a link of the file base points to, false when it's not in the epub.
func epubRef(base, href string) (string, bool) {
	u, err := url.Parse(href)
	if err != nil || href == "" || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", false
	}
	if strings.HasPrefix(u.Path, "/") {
		return strings.TrimPrefix(path.Clean(u.Path), "/"), true
	}
	return path.Join(path.Dir(base), u.Path), true
}

// epubDocImages are the images a page document shows, in its order (img, svg image).
func epubDocImages(r *zip.Reader, doc string) []string {
	f, err := r.Open(doc)
	if err != nil {
		return nil
	}
	defer f.Close()
	var images []string
	z := html.NewTokenizer(io.LimitReader(f, epubReadMax))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return images
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		if t.Data != "img" && t.Data != "image" {
			continue
		}
		for _, a := range t.Attr {
			if a.Key == "src" || a.Key == "href" || a.Key == "xlink:href" {
				if image, ok := epubRef(doc, a.Val); ok {
					images = append(images, image)
				}
				break
			}
		}
	}
}

// inSpine are the pages in the spine's order, each once.
func inSpine(pages []Page, spine []string) []Page {
	byName := make(map[string]Page, len(pages))
	for _, p := range pages {
		byName[p.Name] = p
	}
	var out []Page
	for _, name := range spine {
		if p, ok := byName[name]; ok {
			out = append(out, p)
			delete(byName, name)
		}
	}
	return out
}
