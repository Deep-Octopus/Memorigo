package storage

import (
	"errors"
)

type Manager struct {
	adapter Adapter
	driver  Driver
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start(conn any) error {
	if conn == nil {
		return nil
	}
	a, err := RegistryAdapter(conn)
	if err != nil {
		return err
	}
	d, err := RegistryDriver(a)
	if err != nil {
		return err
	}
	m.adapter = a
	m.driver = d
	return nil
}

func (m *Manager) Adapter() Adapter { return m.adapter }
func (m *Manager) Driver() Driver   { return m.driver }
func (m *Manager) Dialect() string {
	if m.adapter == nil {
		return ""
	}
	return m.adapter.Dialect()
}

func (m *Manager) Build() error {
	if m.driver == nil {
		return nil
	}
	return m.driver.Migrate()
}

var ErrNoAdapter = errors.New("no adapter registered for connection type")


