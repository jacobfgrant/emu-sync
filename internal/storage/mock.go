package storage

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// MockBackend is an in-memory Backend for testing.
type MockBackend struct {
	mu      sync.Mutex
	Objects map[string][]byte // key -> content
	Calls   []string          // log of method calls for assertions
	// Set to simulate errors on specific keys
	UploadErrors   map[string]error
	DownloadErrors map[string]error
	DeleteErrors   map[string]error
}

// NewMockBackend creates a MockBackend with initialized maps.
func NewMockBackend() *MockBackend {
	return &MockBackend{
		Objects:        make(map[string][]byte),
		UploadErrors:   make(map[string]error),
		DownloadErrors: make(map[string]error),
		DeleteErrors:   make(map[string]error),
	}
}

func (m *MockBackend) Ping(_ context.Context) error {
	return nil
}

func (m *MockBackend) UploadFile(_ context.Context, key, localPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, "UploadFile:"+key)

	if err, ok := m.UploadErrors[key]; ok {
		return err
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	m.Objects[key] = data
	return nil
}

func (m *MockBackend) UploadBytes(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, "UploadBytes:"+key)

	if err, ok := m.UploadErrors[key]; ok {
		return err
	}

	m.Objects[key] = data
	return nil
}

func (m *MockBackend) DownloadFile(_ context.Context, key, localPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, "DownloadFile:"+key)

	if err, ok := m.DownloadErrors[key]; ok {
		return err
	}

	data, ok := m.Objects[key]
	if !ok {
		return fmt.Errorf("object not found: %s", key)
	}

	return os.WriteFile(localPath, data, 0o644)
}

func (m *MockBackend) DownloadBytes(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, "DownloadBytes:"+key)

	if err, ok := m.DownloadErrors[key]; ok {
		return nil, err
	}

	data, ok := m.Objects[key]
	if !ok {
		return nil, fmt.Errorf("object not found: %s", key)
	}

	return data, nil
}

func (m *MockBackend) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, "DeleteObject:"+key)

	if err, ok := m.DeleteErrors[key]; ok {
		return err
	}

	delete(m.Objects, key)
	return nil
}

func (m *MockBackend) DownloadManifest(ctx context.Context) ([]byte, error) {
	return m.DownloadBytes(ctx, ManifestKey)
}

func (m *MockBackend) UploadManifest(ctx context.Context, data []byte) error {
	return m.UploadBytes(ctx, ManifestKey, data)
}
