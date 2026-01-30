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

// Implement service.ServerInfo interface
func (r *RemoteServerInfo) GetServerID() string {
	return r.ServerID
}

func (r *RemoteServerInfo) GetHost() string {
	return r.Host
}

func (r *RemoteServerInfo) GetAPIKey() string {
	return r.APIKey
}

func (r *RemoteServerInfo) GetAPISecret() string {
	return r.APISecret
}

// Implement service.RoomInfo interface
func (r *RemoteRoomInfo) GetRoomSID() string {
	return r.RoomSID
}

func (r *RemoteRoomInfo) GetRoomName() string {
	return r.RoomName
}
