// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/config"
	"github.com/livekit/livekit-server/pkg/gateway"
)

func setupTestRegistry(t *testing.T) (*gateway.ServerRegistry, *miniredis.Miniredis, func()) {
	mr := miniredis.RunT(t)
	rc := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{mr.Addr()},
	})

	// 32-byte encryption key for testing
	encryptionKey := []byte("12345678901234567890123456789012")

	staticServers := map[string]*config.RemoteServerConfig{
		"static-server": {
			Host:      "https://static.example.com",
			APIKey:    "static-key",
			APISecret: "static-secret",
		},
	}

	registry, err := gateway.NewServerRegistry(rc, encryptionKey, staticServers)
	require.NoError(t, err)

	cleanup := func() {
		rc.Close()
		mr.Close()
	}

	return registry, mr, cleanup
}

func TestServerRegistry_RegisterAndGet(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Register a server
	info := &gateway.RemoteServerInfo{
		ServerID:  "test-server",
		Host:      "https://test.example.com",
		APIKey:    "test-api-key",
		APISecret: "test-api-secret",
		CreatedAt: time.Now(),
		CreatedBy: "test-user",
	}

	err := registry.RegisterServer(ctx, info)
	require.NoError(t, err)

	// Retrieve the server
	retrieved, err := registry.GetServer(ctx, "test-server")
	require.NoError(t, err)
	require.Equal(t, info.ServerID, retrieved.ServerID)
	require.Equal(t, info.Host, retrieved.Host)
	require.Equal(t, info.APIKey, retrieved.APIKey)
	require.Equal(t, info.APISecret, retrieved.APISecret)
	require.Equal(t, info.CreatedBy, retrieved.CreatedBy)
}

func TestServerRegistry_GetStaticServer(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Retrieve static server
	retrieved, err := registry.GetServer(ctx, "static-server")
	require.NoError(t, err)
	require.Equal(t, "static-server", retrieved.ServerID)
	require.Equal(t, "https://static.example.com", retrieved.Host)
	require.Equal(t, "static-key", retrieved.APIKey)
	require.Equal(t, "static-secret", retrieved.APISecret)
	require.Equal(t, "config", retrieved.CreatedBy)
}

func TestServerRegistry_GetNonExistent(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Try to get non-existent server
	_, err := registry.GetServer(ctx, "non-existent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "server not found")
}

func TestServerRegistry_ListServers(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Register a dynamic server
	info := &gateway.RemoteServerInfo{
		ServerID:  "dynamic-server",
		Host:      "https://dynamic.example.com",
		APIKey:    "dynamic-key",
		APISecret: "dynamic-secret",
		CreatedAt: time.Now(),
		CreatedBy: "test-user",
	}
	err := registry.RegisterServer(ctx, info)
	require.NoError(t, err)

	// List all servers
	servers, err := registry.ListServers(ctx)
	require.NoError(t, err)
	require.Len(t, servers, 2) // static + dynamic
	require.Contains(t, servers, "static-server")
	require.Contains(t, servers, "dynamic-server")
}

func TestServerRegistry_DeleteServer(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Register a server
	info := &gateway.RemoteServerInfo{
		ServerID:  "delete-me",
		Host:      "https://delete.example.com",
		APIKey:    "delete-key",
		APISecret: "delete-secret",
		CreatedAt: time.Now(),
		CreatedBy: "test-user",
	}
	err := registry.RegisterServer(ctx, info)
	require.NoError(t, err)

	// Delete the server
	err = registry.DeleteServer(ctx, "delete-me")
	require.NoError(t, err)

	// Verify it's gone
	_, err = registry.GetServer(ctx, "delete-me")
	require.Error(t, err)
}

func TestServerRegistry_CannotDeleteStaticServer(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Try to delete static server
	err := registry.DeleteServer(ctx, "static-server")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot delete static config server")
}

func TestServerRegistry_EncryptionDecryption(t *testing.T) {
	registry, _, cleanup := setupTestRegistry(t)
	defer cleanup()

	ctx := context.Background()

	// Register with sensitive data
	info := &gateway.RemoteServerInfo{
		ServerID:  "encryption-test",
		Host:      "https://test.example.com",
		APIKey:    "super-secret-key-12345",
		APISecret: "super-secret-secret-67890",
		CreatedAt: time.Now(),
		CreatedBy: "test-user",
	}
	err := registry.RegisterServer(ctx, info)
	require.NoError(t, err)

	// Retrieve and verify decryption worked
	retrieved, err := registry.GetServer(ctx, "encryption-test")
	require.NoError(t, err)
	require.Equal(t, info.APIKey, retrieved.APIKey)
	require.Equal(t, info.APISecret, retrieved.APISecret)
}

func TestServerRegistry_InvalidKeyLength(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	rc := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{mr.Addr()},
	})
	defer rc.Close()

	// Try with wrong key length
	shortKey := []byte("short")
	_, err := gateway.NewServerRegistry(rc, shortKey, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be 32 bytes")
}
