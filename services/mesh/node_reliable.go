package mesh

// node_reliable.go — the reliable-delivery side of a mesh node.
//
// node.go owns routing, discovery and store-and-forward. This file adds the
// layer that turns best-effort forwarding into an accountable transfer: a
// payload is retained until the destination acknowledges it, retries take an
// alternate path with exponential backoff, and the outcome is reported as an
// explicit delivery state (acked / expired / dead_letter) instead of silence.
//
// Group payloads deliberately remain best-effort. Acknowledging a group
// message needs one ACK per member and per-member group key management, which
// the mesh analyses list as separate outstanding work; pretending otherwise
// here would report a "delivered" state the protocol cannot actually prove.

import (
	"errors"
	"time"
)

// reliabilityTick is how often the sender-side retry state machine advances.
const reliabilityTick = time.Second

// flushBudget bounds how many packets one flush pass hands to the radio, so a
// deep queue cannot monopolise the transport goroutine.
const flushBudget = 64

// SendReliable sends a unicast payload with acknowledgement and bounded
// retries. It returns the transfer id, which the caller uses to read the
// delivery state (see Transfer) and to correlate a UI "delivered" indicator.
//
// The payload must fit the reliable-transfer ceiling; larger payloads use the
// fragmentation path, whose parts are individually best-effort.
func (n *Node) SendReliable(kind PacketKind, dst string, plaintext []byte) (string, error) {
	if dst == "" {
		return "", errors.New("mesh: destination is required")
	}
	if kind == KindAck {
		return "", errors.New("mesh: acknowledgements are generated internally")
	}
	if len(plaintext) == 0 {
		return "", errors.New("mesh: empty payload")
	}
	if len(plaintext) > maxReliablePayload {
		return "", ErrTransferTooLarge
	}
	tr := n.transfers.Create(dst, kind, PriorityForKind(kind), plaintext)
	// Drive the first attempt through the SAME accounting the retry loop uses,
	// so the transfer's attempt count and state reflect a real transmission
	// immediately instead of claiming to be "awaiting first send" until the
	// next tick. A transient transport failure is not reported to the caller:
	// the transfer stays live and Tick retries it.
	n.Tick()
	return tr.ID, nil
}

// transmit encrypts a payload for one transmission attempt and queues it. Each
// attempt is a fresh packet with a fresh id and a fresh AEAD nonce, so a retry
// is never suppressed by intermediate-node duplicate suppression and no two
// transmissions of a transfer ever reuse a (key, nonce) pair.
func (n *Node) transmit(tr *Transfer, plaintext []byte) error {
	key := n.sessionKeyFor(tr.Dst)
	ct, nonce, err := Encrypt(key, plaintext)
	if err != nil {
		return err
	}
	p := NewPacket(tr.Kind, n.DeviceID, tr.Dst, n.maxHops)
	p.Payload = ct
	p.Nonce = nonce
	p.Seq = n.nextSeq()
	p.Xfer = tr.ID
	n.routes.Seen(p.ID)
	if err := n.pfifo.Enqueue(p); err != nil {
		return err
	}
	n.transfers.NotePacket(tr.ID, p.ID)
	return nil
}

// sendAck returns an acknowledgement for a settled transfer to its origin.
// Acks are control-class, so a congested relay still returns them promptly.
func (n *Node) sendAck(dst, transferID string) {
	if dst == "" || transferID == "" {
		return
	}
	p := NewPacket(KindAck, n.DeviceID, dst, n.maxHops)
	proof := []byte("chatapp-mesh-ack-v1:" + transferID)
	ct, nonce, err := Encrypt(n.sessionKeyFor(dst), proof)
	if err != nil {
		return
	}
	p.AckFor = transferID
	p.Xfer = transferID
	p.Payload = ct
	p.Nonce = nonce
	p.Seq = n.nextSeq()
	n.routes.Seen(p.ID)
	if err := n.pfifo.Enqueue(p); err == nil {
		n.flush()
	}
}

// Tick advances the retry state machine once and transmits every transfer that
// is due. It returns how many attempts were queued. Exposed so tests and
// tooling can drive the machine deterministically instead of sleeping.
func (n *Node) Tick() int {
	due := n.transfers.Tick()
	sent := 0
	for _, tr := range due {
		pt := n.transfers.Payload(tr.ID)
		if pt == nil {
			continue
		}
		if err := n.transmit(tr, pt); err != nil {
			n.transfers.NoteError(tr.ID, err)
			continue
		}
		sent++
	}
	// Flush on every tick, not only after a retry is transmitted. This lets
	// congestion-blocked and route-waiting packets recover as tokens refill or
	// a beacon installs a new neighbour.
	n.flush()
	return sent
}

// reliabilityLoop advances the retry machine on a fixed cadence.
func (n *Node) reliabilityLoop() {
	ticker := time.NewTicker(reliabilityTick)
	defer ticker.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-ticker.C:
			n.Tick()
		}
	}
}

// Transfer returns the delivery state of one transfer.
func (n *Node) Transfer(id string) (TransferView, bool) { return n.transfers.Get(id) }

// Transfers returns every retained transfer, newest first.
func (n *Node) Transfers() []TransferView { return n.transfers.Snapshot() }

// PendingTransfers reports how many transfers are not yet terminal.
func (n *Node) PendingTransfers() int { return n.transfers.Pending() }

// DeliveryCounts reports how many transfers sit in each delivery state.
func (n *Node) DeliveryCounts() map[string]int { return n.transfers.Counts() }

// QueueStatus reports the forwarding buffer's occupancy and pressure counters.
// It is the honest view of a relay under load: bytes held, packets held, drain
// depth per traffic class, and how many packets were dropped or expired.
func (n *Node) QueueStatus() map[string]any {
	return map[string]any{
		"packets":          n.pfifo.Len(),
		"bytes":            n.pfifo.Bytes(),
		"dropped":          n.pfifo.Dropped(),
		"depth_by_class":   n.pfifo.DepthByPriority(),
		"pending_transfer": n.transfers.Pending(),
		"delivery_counts":  n.transfers.Counts(),
		"congestion":       n.congestion.Status(),
	}
}

// SetNow overrides this node's clock (and its retry tracker's clock). Tests use
// it to drive delivery states deterministically instead of sleeping.
func (n *Node) SetNow(now func() time.Time) {
	n.transfers.SetNow(now)
}

// RetryAttempts reports how many transmissions have been made for a transfer.
func (n *Node) RetryAttempts(id string) int {
	view, ok := n.transfers.Get(id)
	if !ok {
		return 0
	}
	return view.Attempts
}
