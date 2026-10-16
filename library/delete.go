package library

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// DeleteManga is DeleteMangaContext with a background ctx.
func (f Files) DeleteManga(m storage.Manga) error {
	return f.DeleteMangaContext(context.Background(), m)
}

// DeleteMangaContext moves the manga's folder to the trash (only its recorded downloads
// when it shares the folder) and forgets it; a move across drives reports its copy to ctx.
func (f Files) DeleteMangaContext(ctx context.Context, m storage.Manga) error {
	batch := newTrashBatch()
	if m.Dir == "" {
		downloads, err := f.Library.Downloads(m.ID)
		if err != nil {
			return err
		}
		for _, dl := range downloads {
			if err := f.trashIfExists(ctx, batch, dl.Path); err != nil {
				return err
			}
		}
	} else if err := f.trashIfExists(ctx, batch, m.Path()); err != nil {
		return err
	}
	return f.Library.RemoveManga(m.ID)
}

// DeleteDownload is DeleteDownloadContext with a background ctx.
func (f Files) DeleteDownload(m storage.Manga, dl storage.Download) error {
	return f.DeleteDownloadContext(context.Background(), m, dl)
}

// DeleteDownloadContext moves a chapter file to the trash and forgets it, and removes the
// volume folder it was in if that's now empty.
func (f Files) DeleteDownloadContext(ctx context.Context, m storage.Manga, dl storage.Download) error {
	if err := f.trashIfExists(ctx, newTrashBatch(), dl.Path); err != nil {
		return err
	}
	if err := f.Library.RemoveDownload(dl.Path); err != nil {
		return err
	}
	dir := filepath.Dir(dl.Path)
	if m.Dir == "" || dir == m.Path() {
		return nil
	}
	if empty, err := afero.IsEmpty(f.FS, dir); err == nil && empty {
		return removeDir(f.FS, f.Journal, dir)
	}
	return nil
}

func (f Files) trashIfExists(ctx context.Context, batch, path string) error {
	exists, err := afero.Exists(f.FS, path)
	if err != nil || !exists {
		return err
	}
	if err := moveToTrash(ctx, f.FS, f.Journal, f.Root, batch, path, f.Modes); err != nil {
		return fmt.Errorf("trash %s: %w", path, err)
	}
	return nil
}
