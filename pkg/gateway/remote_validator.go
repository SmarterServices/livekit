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
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type RemoteRoomInfo struct {
	RoomSID  string
	RoomName string
}

type CachedRoomInfo struct {
	RoomSID   string
	RoomName  string
	ExpiresAt time.Time
}

type RemoteValidator struct {
	registry   *ServerRegistry
	cache      *sync.Map
	cacheTTL   time.Duration
	httpClient *http.Client
}

func NewRemoteValidator(registry *ServerRegistry, cacheTTL, timeout time.Duration) *RemoteValidator {
	return &RemoteValidator{
		registry: registry,
		cache:    &sync.Map{},
		cacheTTL: cacheTTL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
		},
	}
}

// ValidateRoom validates a room exists on the remote media server
func (v *RemoteValidator) ValidateRoom(ctx context.Context, serverID, roomName string) (*RemoteRoomInfo, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%s:%s", serverID, roomName)
	if cached, ok := v.cache.Load(cacheKey); ok {
		info := cached.(*CachedRoomInfo)
		if time.Now().Before(info.ExpiresAt) {
			// Cache hit
			return &RemoteRoomInfo{
				RoomSID:  info.RoomSID,
				RoomName: info.RoomName,
			}, nil
		}
		// Expired - remove from cache
		v.cache.Delete(cacheKey)
	}

	// Cache miss - get server credentials from registry
	server, err := v.registry.GetServer(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to get server credentials: %w", err)
	}

	// Validate room on remote server
	roomInfo, err := v.validateRemote(ctx, server, roomName)
	if err != nil {
		return nil, err
	}

	// Cache the result
	v.cache.Store(cacheKey, &CachedRoomInfo{
		RoomSID:   roomInfo.RoomSID,
		RoomName:  roomInfo.RoomName,
		ExpiresAt: time.Now().Add(v.cacheTTL),
	})

	return roomInfo, nil
}

func (v *RemoteValidator) validateRemote(ctx context.Context, server *RemoteServerInfo, roomName string) (*RemoteRoomInfo, error) {
	// Create LiveKit client for remote server
	roomClient := lksdk.NewRoomServiceClient(server.Host, server.APIKey, server.APISecret)

	// List rooms to check if it exists
	res, err := roomClient.ListRooms(ctx, &livekit.ListRoomsRequest{
		Names: []string{roomName},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to validate room on remote server: %w", err)
	}

	if len(res.Rooms) == 0 {
		return nil, fmt.Errorf("room not found on remote server: %s", roomName)
	}

	return &RemoteRoomInfo{
		RoomSID:  res.Rooms[0].Sid,
		RoomName: res.Rooms[0].Name,
	}, nil
}

// ClearCache clears all cached room validations
func (v *RemoteValidator) ClearCache() {
	v.cache = &sync.Map{}
}

// ClearCacheForServer clears cached validations for a specific server
func (v *RemoteValidator) ClearCacheForServer(serverID string) {
	v.cache.Range(func(key, value interface{}) bool {
		cacheKey := key.(string)
		if len(cacheKey) > len(serverID) && cacheKey[:len(serverID)] == serverID {
			v.cache.Delete(key)
		}
		return true
	})
}
