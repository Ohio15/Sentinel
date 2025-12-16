// +build !windows

package admin

import "errors"

var errNotSupported = errors.New("admin management not supported on this platform")

func (m *Manager) discoverAdmins() ([]AdminAccount, error) {
	return nil, errNotSupported
}

func (m *Manager) getCurrentUser() (*AdminAccount, error) {
	return nil, errNotSupported
}

func (m *Manager) isDomainJoined() bool {
	return false
}

func (m *Manager) removeFromAdministrators(sidStr string) error {
	return errNotSupported
}

func (m *Manager) addToUsers(sidStr string) error {
	return errNotSupported
}

func (m *Manager) addToAdministratorsByName(name string) error {
	return errNotSupported
}
