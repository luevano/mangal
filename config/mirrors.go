package config

import "maps"

// SetMirror writes the provider's preferred mirror to the config file, an empty mirror
// removes it. Like WriteKey, the other keys keep their values.
func SetMirror(providerID, mirror string) error {
	mirrors := maps.Clone(Providers.Mirrors.Get())
	if mirror == "" {
		delete(mirrors, providerID)
	} else {
		mirrors[providerID] = mirror
	}
	return Providers.Mirrors.Write(mirrors)
}
