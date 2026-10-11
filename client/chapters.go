package client

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

// MangaChapters are the chapters of every volume of the manga, in order.
func MangaChapters(ctx context.Context, client *libmangal.Client, manga mangadata.Manga) ([]mangadata.Chapter, error) {
	volumes, err := client.MangaVolumesContext(ctx, manga)
	if err != nil {
		return nil, fmt.Errorf("get volumes of %q: %w", manga, err)
	}
	var chapters []mangadata.Chapter
	for _, volume := range volumes {
		c, err := client.VolumeChaptersContext(ctx, volume)
		if err != nil {
			return nil, fmt.Errorf("get chapters of %q: %w", volume, err)
		}
		chapters = append(chapters, c...)
	}
	return chapters, nil
}
