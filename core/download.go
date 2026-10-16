package core

import (
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/path"
)

// DownloadOptions are the config's download options. For reading (read) the chapter is
// in the read format and, without read.download_on_read, goes to the temp directory with
// every subdirectory.
func DownloadOptions(read bool) libmangal.DownloadOptions {
	options := config.DownloadOptions()
	if !read {
		return options
	}
	options.Format = config.Read.Format.Get()
	if !config.Read.DownloadOnRead.Get() {
		options.Directory = path.Temp()
		options.CreateProviderDir = true
		options.CreateMangaDir = true
		options.CreateVolumeDir = true
	}
	return options
}
