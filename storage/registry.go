package storage

import (
	"fmt"
)

type Adapter interface {
	Dialect() string
}

type Driver interface {
	Dialect() string
	Migrate() error
}

type adapterMatcher func(conn any) bool
type adapterFactory func(conn any) (Adapter, error)
type driverFactory func(adapter Adapter) (Driver, error)

var (
	adapterRegistry = make([]struct {
		match   adapterMatcher
		factory adapterFactory
	}, 0)
	driverRegistry = make(map[string]driverFactory)
)

func RegisterAdapter(match adapterMatcher, factory adapterFactory) {
	adapterRegistry = append(adapterRegistry, struct {
		match   adapterMatcher
		factory adapterFactory
	}{match: match, factory: factory})
}

func RegisterDriver(dialect string, factory driverFactory) {
	driverRegistry[dialect] = factory
}

func RegistryAdapter(conn any) (Adapter, error) {
	for _, entry := range adapterRegistry {
		if entry.match(conn) {
			return entry.factory(conn)
		}
	}
	return nil, fmt.Errorf("%w: %T", ErrNoAdapter, conn)
}

func RegistryDriver(adapter Adapter) (Driver, error) {
	dialect := adapter.Dialect()
	f, ok := driverRegistry[dialect]
	if !ok {
		return nil, fmt.Errorf("no driver registered for dialect: %s", dialect)
	}
	return f(adapter)
}


