package storage

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/ije/gox/utils"
)

type LocalLRUFS struct{}

func maxCostValueFrom(query url.Values) (int64, error) {
	maxCostString := query.Get("maxCost")
	if maxCostString != "" {
		return utils.ParseBytes(maxCostString)
	}
	return 1 << 30, nil // Default maximum cost of cache is 1GB
}

func (fs *LocalLRUFS) Open(fsUrl string) (FSConn, error) {
	config, err := url.Parse(fsUrl)
	if err != nil {
		return nil, err
	}
	maxCost, err := maxCostValueFrom(config.Query())
	if err != nil {
		return nil, err
	}
	backing, err := OpenFS("local:" + config.Path)
	if err != nil {
		return nil, err
	}

	remove := func(name string) {
		go os.Remove(path.Join(config.Path, name))
	}

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7, // number of keys to track frequency of (10M).
		MaxCost:     maxCost,
		BufferItems: 64, // number of keys per Get buffer.
		/**
		 * Although tempting to use, OnExit is called when items are replaced,
		 * which would result in unnecessary removals from the backing fs.
		 */
		OnEvict: func(item *ristretto.Item) {
			cached := item.Value.(*localLRUFSCachedValue)
			log.Debugf("localLRU OnEvict %s", cached.name)
			remove(cached.name)
		},
		OnReject: func(item *ristretto.Item) {
			cached := item.Value.(*localLRUFSCachedValue)
			log.Debugf("localLRU OnReject %s", cached.name)
			remove(cached.name)
		},
		Metrics: isDev,
	})
	if err != nil {
		return nil, err
	}

	// WIP: don't think it needs a TTL with a MaxCost, just preparing the logic for it.
	const TTL time.Duration = 0 * time.Second //time.Duration(30) * time.Minute,

	// Hydrate the cache on Open
	log.Debugf("localLRU root %s, maxCost %d, hydrating...", config.Path, maxCost)
	filepath.WalkDir(config.Path,
		func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			name, err := filepath.Rel(config.Path, path)
			if err != nil {
				return err
			}
			cost := info.Size()
			modtime := info.ModTime()
			if !cache.SetWithTTL(name, &localLRUFSCachedValue{name: name, modtime: modtime}, cost, TTL) {
				remove(name)
			}
			return nil
		})

	cache.Wait()

	log.Debugf("localLRU hydrated")
	if isDev {
		log.Debugf("localLRU metrics %s", cache.Metrics.String())
		cache.Metrics.Clear()
	}

	return &localLRUFSLayer{
		backing: backing,
		cache:   cache,
		remove:  remove,
		TTL:     TTL,
	}, nil
}

type localLRUFSLayer struct {
	backing FSConn
	cache   *ristretto.Cache
	remove  func(string)
	TTL     time.Duration
}

type localLRUFSCachedValue struct {
	name    string
	modtime time.Time
}

func (fs *localLRUFSLayer) Exists(name string) (found bool, modtime time.Time, err error) {
	fs.cache.Wait()
	value, found := fs.cache.Get(name)
	if found {
		cached := value.(*localLRUFSCachedValue)
		modtime = cached.modtime
	}
	return
}

func (fs *localLRUFSLayer) ReadFile(name string) (file io.ReadSeekCloser, err error) {
	_, found := fs.cache.Get(name)
	if found {
		file, err = fs.backing.ReadFile(name)
		if err == nil {
			return
		}
		// If for some reason we can't read the backing store, make sure we remove from cache
		fs.cache.Del(name)
	}
	err = fmt.Errorf("%s unexpectedly missing", name)
	return
}

func (fs *localLRUFSLayer) WriteFile(name string, content io.Reader) (written int64, err error) {
	written, err = fs.backing.WriteFile(name, content)
	if err != nil {
		return
	}

	if !fs.cache.SetWithTTL(name, &localLRUFSCachedValue{name: name, modtime: time.Now()}, written, fs.TTL) {
		fs.remove(name)
		return 0, fmt.Errorf("rejected storing %s", name)
	}
	log.Debugf("localLRU accepted %s, cost %d", name, written)
	return
}

func (fs *localLRUFSLayer) WriteData(name string, data []byte) (err error) {
	cost := int64(len(data))
	if !fs.cache.SetWithTTL(name, &localLRUFSCachedValue{name: name, modtime: time.Now()}, cost, fs.TTL) {
		return fmt.Errorf("rejected storing %s", name)
	}

	err = fs.backing.WriteData(name, data)
	if err != nil {
		fs.cache.Del(name)
		return err
	}
	log.Debugf("localLRU accepted %s, cost %d", name, cost)
	return
}

func init() {
	RegisterFS("localLRU", &LocalLRUFS{})
}