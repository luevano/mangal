package base

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
)

// MetadataSearcher searches the source: through the server on one (its login, its rate limits,
// browse), else on this machine. An error when the source isn't enabled there.
func MetadataSearcher(src client.MetadataSource) (client.MetadataSearcher, error) {
	if r := CurrentRemote; r != nil {
		if !slices.Contains(r.MetadataSources, string(src.Code)) {
			return nil, fmt.Errorf("%s isn't enabled on %s", src.Name, r.Name())
		}
		return r.Client.Metadata(src), nil
	}
	return client.MetadataProvider(src.Source)
}

// RemoteMetadata is the source's manga with the id through the server. Without browse there it
// asks nothing and has none: on a server only the links' ids are known then.
func (r *Remote) RemoteMetadata(ctx context.Context, src client.MetadataSource, id string) (lmmeta.Metadata, error) {
	if !r.Has(core.ScopeBrowse) || !slices.Contains(r.MetadataSources, string(src.Code)) {
		return nil, nil
	}
	n, err := strconv.Atoi(id)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("%s id %q isn't a positive integer", src.Name, id)
	}
	meta, found, err := r.Client.Metadata(src).SearchByIDContext(ctx, n)
	if err != nil || !found {
		return nil, err
	}
	return meta, nil
}
