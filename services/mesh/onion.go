package mesh

import (
	"encoding/json"
	"errors"
)

// onionEnvelope is the authenticated plaintext revealed by exactly one hop.
// A relay learns only the next hop and the next ciphertext. The final envelope
// carries the application payload and is revealed only to the destination.
type onionEnvelope struct {
	NextHop string     `json:"next_hop,omitempty"`
	Final   bool       `json:"final"`
	Inner   []byte     `json:"inner,omitempty"`
	Dst     string     `json:"dst,omitempty"`
	Kind    PacketKind `json:"kind,omitempty"`
	GroupID string     `json:"group_id,omitempty"`
	Payload []byte     `json:"payload,omitempty"`
	Nonce   []byte     `json:"nonce,omitempty"`
}

type sealedOnion struct {
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

func sealOnion(key *IdentityKey, e onionEnvelope) ([]byte, error) {
	plain, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	data, nonce, err := Encrypt(key, plain)
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealedOnion{Nonce: nonce, Data: data})
}

func openOnion(key *IdentityKey, wire []byte) (onionEnvelope, error) {
	var sealed sealedOnion
	if err := json.Unmarshal(wire, &sealed); err != nil {
		return onionEnvelope{}, err
	}
	plain, err := Decrypt(key, sealed.Data, sealed.Nonce)
	if err != nil {
		return onionEnvelope{}, err
	}
	var e onionEnvelope
	if err := json.Unmarshal(plain, &e); err != nil {
		return onionEnvelope{}, err
	}
	if e.Final && e.Dst == "" {
		return onionEnvelope{}, errors.New("onion final envelope has no destination")
	}
	if !e.Final && e.NextHop == "" {
		return onionEnvelope{}, errors.New("onion relay envelope has no next hop")
	}
	return e, nil
}

// buildOnion constructs an onion for path[0] (the sender) through every relay
// to path[len-1] (the destination). Every layer is encrypted with a distinct
// pairwise session key; a relay never receives the final route.
func (n *Node) buildOnion(kind PacketKind, src string, path []string, payload, nonce []byte, groupID string) ([]byte, error) {
	if len(path) < 2 || path[0] != src {
		return nil, errors.New("onion path must contain sender and destination")
	}
	inner, err := sealOnion(n.onionKeyFor(path[len(path)-1]), onionEnvelope{
		Final: true, Dst: path[len(path)-1], Kind: kind, GroupID: groupID,
		Payload: payload, Nonce: nonce,
	})
	if err != nil {
		return nil, err
	}
	for i := len(path) - 3; i >= 0; i-- {
		inner, err = sealOnion(n.onionKeyFor(path[i+1]), onionEnvelope{
			NextHop: path[i+2], Inner: inner,
		})
		if err != nil {
			return nil, err
		}
	}
	return inner, nil
}

func (n *Node) onionKeyFor(peer string) *IdentityKey {
	n.mu.Lock()
	adv, ok := n.peerKEM[peer]
	n.mu.Unlock()
	if !ok {
		return nil
	}
	key, err := SessionKey(n.kem, n.DeviceID, adv.Public, peer)
	if err != nil {
		return nil
	}
	return key
}

// SendVia sends a packet over an explicit authenticated path. This is the
// production privacy-preserving path; callers may use routing simulation to
// select path before invoking it. It refuses to downgrade to a shared key.
func (n *Node) SendVia(kind PacketKind, path []string, plaintext []byte) (string, error) {
	if len(path) < 2 || path[0] != n.DeviceID {
		return "", errors.New("invalid onion path")
	}
	for _, peer := range path[1:] {
		if n.onionKeyFor(peer) == nil {
			return "", errors.New("missing authenticated session key for onion hop")
		}
	}
	p := NewPacket(kind, n.DeviceID, path[1], n.maxHops)
	p.HopSrc = n.DeviceID
	p.Seq = n.nextSeq()
	key := n.sessionKeyFor(path[len(path)-1])
	ct, nonce, err := Encrypt(key, plaintext)
	if err != nil {
		return "", err
	}
	onion, err := n.buildOnion(kind, n.DeviceID, path, ct, nonce, "")
	if err != nil {
		return "", err
	}
	p.Onion = onion
	p.Payload = nil
	p.Nonce = nil
	n.routes.Seen(p.ID)
	if err := n.pfifo.Enqueue(p); err != nil {
		return "", err
	}
	n.flush()
	return p.ID, nil
}

// peelOnion authenticates and peels the layer sent by p.HopSrc. It returns
// false for malformed, forged, or downgraded packets.
func (n *Node) peelOnion(p *Packet) bool {
	if len(p.Onion) == 0 || p.HopSrc == "" {
		return false
	}
	key := n.onionKeyFor(p.HopSrc)
	if p.HopSrc != n.DeviceID && p.Dst == n.DeviceID {
		// The final layer is sealed to the destination using the origin's
		// authenticated session. This lets the origin construct the complete
		// envelope without learning any relay private keys.
		key = n.onionKeyFor(p.Src)
	}
	if key == nil {
		return false
	}
	e, err := openOnion(key, p.Onion)
	if err != nil {
		return false
	}
	if e.Final {
		if e.Dst != n.DeviceID {
			return false
		}
		p.Dst, p.Kind, p.GroupID = e.Dst, e.Kind, e.GroupID
		p.Payload, p.Nonce, p.Onion = e.Payload, e.Nonce, nil
		p.OnionFinal = true
		return true
	}
	p.HopSrc, p.Dst, p.Onion = n.DeviceID, e.NextHop, e.Inner
	p.OnionFinal = false
	return true
}
