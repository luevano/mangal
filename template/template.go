// Package template runs the download name templates (config download.*.name_template).
package template

import (
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
)

func init() {
	config.CheckTemplate = Check
}

type mangaTemplateData struct {
	Provider libmangal.ProviderInfo
	Manga    mangadata.MangaInfo
	Metadata metadata.Metadata
}

type volumeTemplateData struct {
	Provider libmangal.ProviderInfo
	Volume   mangadata.VolumeInfo
	Manga    mangadata.MangaInfo
}

type chapterTemplateData struct {
	Provider libmangal.ProviderInfo
	Chapter  mangadata.ChapterInfo
	Volume   mangadata.VolumeInfo
	Manga    mangadata.MangaInfo
}

// parsed templates by text, they're parsed once
var parsed sync.Map

// Parse parses a name template with the template functions.
func Parse(text string) (*template.Template, error) {
	if t, ok := parsed.Load(text); ok {
		return t.(*template.Template), nil
	}
	t, err := template.New("").Funcs(funcMap).Parse(text)
	if err != nil {
		return nil, err
	}
	parsed.Store(text, t)
	return t, nil
}

// Check validates a name template.
func Check(text string) error {
	_, err := Parse(text)
	return err
}

// defaults are libmangal's names, used when a template fails
var defaults = sync.OnceValue(libmangal.DefaultClientOptions)

// execute runs the template of the config key, or returns fallback() if it fails.
func execute(key, text string, data any, fallback func() string) string {
	t, err := Parse(text)
	if err == nil {
		var sb strings.Builder
		if err = t.Execute(&sb, data); err == nil {
			return sb.String()
		}
	}
	name := fallback()
	log.Warn("%s: %s, using %q", key, err, name)
	return name
}

func Provider(provider libmangal.ProviderInfo) string {
	e := config.Download.Provider.NameTemplate
	return execute(e.Key, e.Get(), provider, func() string {
		return defaults().ProviderName(provider)
	})
}

func Manga(provider libmangal.ProviderInfo, manga mangadata.Manga) string {
	// Prioritize the NameTemplate (includes AnilistManga data)
	e := config.Download.Manga.NameTemplateFallback
	meta := manga.Metadata()
	if metadata.Validate(meta) == nil {
		e = config.Download.Manga.NameTemplate
	}
	data := mangaTemplateData{
		Provider: provider,
		Manga:    manga.Info(),
		Metadata: meta,
	}
	return execute(e.Key, e.Get(), data, func() string {
		return defaults().MangaName(provider, manga)
	})
}

func Volume(provider libmangal.ProviderInfo, volume mangadata.Volume) string {
	e := config.Download.Volume.NameTemplate
	data := volumeTemplateData{
		Provider: provider,
		Volume:   volume.Info(),
		Manga:    volume.Manga().Info(),
	}
	return execute(e.Key, e.Get(), data, func() string {
		return defaults().VolumeName(provider, volume)
	})
}

func Chapter(provider libmangal.ProviderInfo, chapter mangadata.Chapter) string {
	e := config.Download.Chapter.NameTemplate
	data := chapterTemplateData{
		Provider: provider,
		Chapter:  chapter.Info(),
		Volume:   chapter.Volume().Info(),
		Manga:    chapter.Volume().Manga().Info(),
	}
	return execute(e.Key, e.Get(), data, func() string {
		return defaults().ChapterName(provider, chapter)
	})
}

// Exec runs a template with the given data, for trying templates out.
func Exec(text string, data any) (string, error) {
	t, err := Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return sb.String(), nil
}
