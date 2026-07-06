//go:build !darwin

package keychain

// MemStore is an in-memory fallback used on non-darwin platforms.
type MemStore struct {
	data map[string]string
}

// New returns an in-memory Store (non-darwin fallback).
func New() Store {
	return &MemStore{data: make(map[string]string)}
}

func (m *MemStore) SetPassword(profileName, password string) error {
	m.data["password:"+profileName] = password
	return nil
}
func (m *MemStore) GetPassword(profileName string) (string, error) {
	v, ok := m.data["password:"+profileName]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (m *MemStore) DeletePassword(profileName string) error {
	delete(m.data, "password:"+profileName)
	return nil
}
func (m *MemStore) SetToken(profileName, token string) error {
	m.data["token:"+profileName] = token
	return nil
}
func (m *MemStore) GetToken(profileName string) (string, error) {
	v, ok := m.data["token:"+profileName]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (m *MemStore) DeleteToken(profileName string) error {
	delete(m.data, "token:"+profileName)
	return nil
}
func (m *MemStore) DeleteAll(profileName string) error {
	m.DeletePassword(profileName)
	m.DeleteToken(profileName)
	return nil
}
