package bcache

import (
	"log"
	"sync"

	"github.com/weaveworks/mesh"
)

const (
	defaultNumEntries = 5
)

// message defines gossip message used for communication between peers
// TODO: use protobuf for encoding
type message struct {
	mux     sync.RWMutex
	PeerID  mesh.PeerName
	Entries map[string]entry
}

// entry is a single key value entry
type entry struct {
	Val     interface{}
	Expired int64
	Deleted int64
}

func newMessage(peerID mesh.PeerName, numEntries int) *message {
	if numEntries == 0 {
		numEntries = defaultNumEntries
	}
	return newMessageFromEntries(peerID, make(map[string]entry, numEntries))
}

func newMessageFromEntries(peerID mesh.PeerName, entries map[string]entry) *message {
	newEntries := make(map[string]entry, len(entries))
	for k, v := range entries {
		newEntries[k] = v
	}
	return &message{
		PeerID:  peerID,
		Entries: newEntries,
	}
}

func newMessageFromBuf(b []byte) (*message, error) {
	var m message
	err := unmarshal(b, &m)
	return &m, err
}

func (m *message) add(key string, val interface{}, expired, deleted int64) {
	m.mux.Lock()
	m.Entries[key] = entry{
		Val:     val,
		Expired: expired,
		Deleted: deleted,
	}
	m.mux.Unlock()
}

// Encode implements mesh.GossipData.Encode
// TODO: split the encoding by X number of keys
func (m *message) Encode() [][]byte {
	m.mux.RLock()
	defer m.mux.RUnlock()

	b, err := marshal(m)
	if err != nil {
		log.Printf("failed to encode message: %v", err)
	}
	return [][]byte{b}
}

// Merge implements mesh.GossipData.Merge
func (m *message) Merge(other mesh.GossipData) (complete mesh.GossipData) {
	return m.mergeComplete(other.(*message))
}

func (m *message) mergeComplete(other *message) mesh.GossipData {
	m.mux.Lock()
	defer m.mux.Unlock()

	for k, v := range other.Entries {
		existing, ok := m.Entries[k]

		// merge
		// - the key not exists in
		// - has less expiration time
		if !ok || existing.Expired < v.Expired {
			m.Entries[k] = v
		}
	}
	return newMessageFromEntries(m.PeerID, m.Entries)
}
