package gocmq

type ICache interface {
	Load(key any) (any, bool)
	Store(key any, value any)
	Delete(key any)
	Range(f func(key, value any) bool)
	Clear()
}

type nullCache struct{}

func (nc *nullCache) Load(_ any) (any, bool)            { return nil, false }
func (nc *nullCache) Store(_ any, _ any)                {}
func (nc *nullCache) Delete(_ any)                      {}
func (nc *nullCache) Range(_ func(key, value any) bool) {}
func (nc *nullCache) Clear()                            {}

// Initialize a default nullCache instance
var defaultCache ICache

func getCache() ICache {
	if defaultCache != nil {
		return defaultCache
	}

	defaultCache = &nullCache{}

	return defaultCache
}
