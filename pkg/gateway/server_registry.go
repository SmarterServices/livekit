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

package gateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/livekit/livekit-server/pkg/config"
)

const (
	remoteServerKeyPrefix = "gateway:remote_server:"
)

type RemoteServerInfo struct {
	ServerID  string    `json:"server_id"`
	Host      string    `json:"host"`
	APIKey    string    `json:"api_key"`
	APISecret string    `json:"api_secret"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

type ServerRegistry struct {
	redis          redis.UniversalClient
	encryptionKey  []byte
	staticServers  map[string]*config.RemoteServerConfig
}

func NewServerRegistry(rc redis.UniversalClient, masterKey []byte, staticServers map[string]*config.RemoteServerConfig) (*ServerRegistry, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("master encryption key must be 32 bytes, got %d", len(masterKey))
	}

	return &ServerRegistry{
		redis:         rc,
		encryptionKey: masterKey,
		staticServers: staticServers,
	}, nil
}

// RegisterServer stores remote server credentials (encrypted)
func (r *ServerRegistry) RegisterServer(ctx context.Context, info *RemoteServerInfo) error {
	// Encrypt credentials before storing
	encryptedKey, err := r.encrypt(info.APIKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt API key: %w", err)
	}

	encryptedSecret, err := r.encrypt(info.APISecret)
	if err != nil {
		return fmt.Errorf("failed to encrypt API secret: %w", err)
	}

	// Store encrypted version
	storedInfo := &RemoteServerInfo{
		ServerID:  info.ServerID,
		Host:      info.Host,
		APIKey:    encryptedKey,
		APISecret: encryptedSecret,
		CreatedAt: info.CreatedAt,
		CreatedBy: info.CreatedBy,
	}

	data, err := json.Marshal(storedInfo)
	if err != nil {
		return err
	}

	key := remoteServerKeyPrefix + info.ServerID
	return r.redis.Set(ctx, key, data, 0).Err()
}

// GetServer retrieves remote server credentials
// Priority: 1) Config file, 2) Redis
func (r *ServerRegistry) GetServer(ctx context.Context, serverID string) (*RemoteServerInfo, error) {
	// Check static config first
	if staticServer, ok := r.staticServers[serverID]; ok {
		return &RemoteServerInfo{
			ServerID:  serverID,
			Host:      staticServer.Host,
			APIKey:    staticServer.APIKey,
			APISecret: staticServer.APISecret,
			CreatedAt: time.Time{}, // Static servers don't have creation time
			CreatedBy: "config",
		}, nil
	}

	// Not in config, check Redis
	key := remoteServerKeyPrefix + serverID

	data, err := r.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("server not found: %s", serverID)
		}
		return nil, err
	}

	var storedInfo RemoteServerInfo
	if err := json.Unmarshal(data, &storedInfo); err != nil {
		return nil, err
	}

	// Decrypt credentials
	apiKey, err := r.decrypt(storedInfo.APIKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	apiSecret, err := r.decrypt(storedInfo.APISecret)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API secret: %w", err)
	}

	return &RemoteServerInfo{
		ServerID:  storedInfo.ServerID,
		Host:      storedInfo.Host,
		APIKey:    apiKey,
		APISecret: apiSecret,
		CreatedAt: storedInfo.CreatedAt,
		CreatedBy: storedInfo.CreatedBy,
	}, nil
}

// ListServers returns all registered server IDs (not credentials)
// Includes both static config servers and Redis servers
func (r *ServerRegistry) ListServers(ctx context.Context) ([]string, error) {
	serverIDs := make([]string, 0)

	// Add static servers from config
	for serverID := range r.staticServers {
		serverIDs = append(serverIDs, serverID)
	}

	// Add dynamic servers from Redis
	keys, err := r.redis.Keys(ctx, remoteServerKeyPrefix+"*").Result()
	if err != nil {
		return serverIDs, nil // Return static servers even if Redis fails
	}

	for _, key := range keys {
		serverID := key[len(remoteServerKeyPrefix):]
		serverIDs = append(serverIDs, serverID)
	}

	return serverIDs, nil
}

// DeleteServer removes a registered server
// Note: Cannot delete static config servers
func (r *ServerRegistry) DeleteServer(ctx context.Context, serverID string) error {
	// Check if it's a static server
	if _, ok := r.staticServers[serverID]; ok {
		return fmt.Errorf("cannot delete static config server: %s", serverID)
	}

	key := remoteServerKeyPrefix + serverID
	return r.redis.Del(ctx, key).Err()
}

// encrypt encrypts a string using AES-256-GCM
func (r *ServerRegistry) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(r.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts a string using AES-256-GCM
func (r *ServerRegistry) decrypt(encrypted string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(r.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}
