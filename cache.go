package gocq

type Cache interface {
	Load(key any) (any, bool)
	Store(key any, value any)
	Delete(key any)
	Clear()
}

type nullCache struct{}

func (nc *nullCache) Load(_ any) (any, bool) { return nil, false }
func (nc *nullCache) Store(_ any, _ any)     {}
func (nc *nullCache) Delete(_ any)           {}
func (nc *nullCache) Clear()                 {}

// Initialize a default nullCache instance
var defaultCache Cache

func getCache() Cache {
	if defaultCache != nil {
		return defaultCache
	}

	defaultCache = &nullCache{}

	return defaultCache
}
