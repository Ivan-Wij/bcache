package bcache

import (
	"github.com/weaveworks/mesh"
)

type peer struct {
	cc       *cache
	name     mesh.PeerName
	send     mesh.Gossip
	actionCh chan func()
	quitCh   chan struct{}
	logger   Logger
}

func newPeer(name mesh.PeerName, maxKeys int, logger Logger) (*peer, error) {
	cc, err := newCache(maxKeys)
	if err != nil {
		return nil, err
	}

	p := &peer{
		cc:       cc,
		name:     name,
		send:     nil, // must be registered
		actionCh: make(chan func()),
		quitCh:   make(chan struct{}),
		logger:   logger,
	}
	go p.loop()
	return p, nil
}

// register the result of a mesh.Router.NewGossip.
func (p *peer) register(send mesh.Gossip) {
	p.actionCh <- func() {
		p.send = send
	}
}

// Gossip implements mesh.Gossiper.Gossip
func (p *peer) Gossip() mesh.GossipData {
	return p.cc.Messages()
}

// OnGossip merges received data into state and returns "everything new
// I've just learnt", or nil if nothing in the received data was new.
//
// It implements mesh.Gossiper.OnGossip
func (p *peer) OnGossip(buf []byte) (delta mesh.GossipData, err error) {
	msg, err := newMessageFromBuf(buf)
	if err != nil {
		return
	}

	var deltaMsg *message

	delta = p.cc.mergeNew(msg)
	if delta != nil {
		deltaMsg = delta.(*message)
	}

	p.logger.Debugf("[%d]OnGossip %v => delta %v", p.name, msg, deltaMsg)
	return
}

// OnGossipBroadcast merges received data into state and returns a
// representation of the received data (typically a delta) for further
// propagation.
//
// It implements mesh.Gossiper.OnGossipBroadcast
func (p *peer) OnGossipBroadcast(src mesh.PeerName, update []byte) (received mesh.GossipData, err error) {
	if src == p.name { // message from ourself, is it possible?
		return
	}
	msg, err := newMessageFromBuf(update)
	if err != nil {
		return
	}

	var recvMsg *message

	received = p.cc.mergeDelta(msg)
	if received != nil {
		recvMsg = received.(*message)
	}
	p.logger.Debugf("[%d]OnGossipBroadcast %v => delta %v", p.name, msg, recvMsg)
	return

}

func (p *peer) OnGossipUnicast(src mesh.PeerName, update []byte) error {
	msg, err := newMessageFromBuf(update)
	if err != nil {
		return err
	}
	p.cc.mergeComplete(msg)
	return nil
}

func (p *peer) Set(key string, val interface{}, expiredTimestamp int64) {
	c := make(chan struct{})

	p.actionCh <- func() {
		defer close(c)

		// set our cache
		p.cc.Set(key, val, expiredTimestamp, 0)

		// construct & send the message
		m := newMessage(p.name, 1)
		m.add(key, val, expiredTimestamp, 0)

		p.broadcast(m)
	}

	<-c // wait for it to be finished
}

func (p *peer) Delete(key string, deleteTimestamp int64) bool {
	var (
		c     = make(chan struct{})
		exist bool
	)

	p.actionCh <- func() {
		defer close(c)

		// delete from our cache
		val, expired, exist := p.cc.Delete(key, deleteTimestamp)
		if !exist {
			return
		}

		// construct & send the message
		m := newMessage(p.name, 1)
		m.add(key, val, expired, deleteTimestamp)

		p.broadcast(m)
	}

	<-c // wait for it to be finished
	return exist
}

func (p *peer) Get(key string) (interface{}, bool) {
	return p.cc.Get(key)
}

func (p *peer) loop() {
	for {
		select {
		case f := <-p.actionCh:
			f()
		case <-p.quitCh:
			return
		}
	}
}

func (p *peer) broadcast(msg *message) {
	if p.send == nil {
		return
	}
	p.send.GossipBroadcast(msg)

}
