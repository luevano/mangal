package client

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/progress"
)

// MangaChapters are the chapters of every volume of the manga, in order.
func MangaChapters(ctx context.Context, client *libmangal.Client, manga mangadata.Manga) ([]mangadata.Chapter, error) {
	volumes, err := client.MangaVolumesContext(ctx, manga)
	if err != nil {
		return nil, fmt.Errorf("get volumes of %q: %w", manga, err)
	}
	var chapters []mangadata.Chapter
	for i, volume := range volumes {
		if len(volumes) > 1 {
			progress.Report(ctx, progress.Status{Source: client.Info().ID, Message: fmt.Sprintf("chapters of %q: volume", manga), Done: i + 1, Total: len(volumes)})
		}
		c, err := client.VolumeChaptersContext(ctx, volume)
		if err != nil {
			return nil, fmt.Errorf("get chapters of %q: %w", volume, err)
		}
		chapters = append(chapters, c...)
	}
	return chapters, nil
}
