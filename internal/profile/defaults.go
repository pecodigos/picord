package profile

import (
	_ "embed"
	"sync"
)

//go:embed defaults.yaml
var embeddedDefaults []byte

var (
	defaultCache []Profile
	defaultOnce  sync.Once
)

func DefaultProfiles() []Profile {
	defaultOnce.Do(func() {
		var err error
		defaultCache, err = DeserializeProfiles(embeddedDefaults)
		if err != nil {
			defaultCache = []Profile{}
		}
		for i := range defaultCache {
			defaultCache[i].isDefault = true
			defaultCache[i].Enabled = true
			if defaultCache[i].Priority == 0 {
				defaultCache[i].Priority = 5
			}
		}
	})
	result := make([]Profile, len(defaultCache))
	copy(result, defaultCache)
	return result
}
