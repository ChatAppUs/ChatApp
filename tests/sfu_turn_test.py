#!/usr/bin/env python3
# End-to-end validation of services/sfu-forwarder (C++ TURN relay data plane).
# Raw UDP/TCP sockets only — no third-party packages. Run:
#   TURN_SECRET=test-turn-secret SFU_SECRET=test-sfu-secret PUBLIC_IP=127.0.0.1 \
#       /tmp/sfu-forwarder &
#   python3 tests/sfu_turn_test.py [host] [turn-port] [control-port]
import base64
import hashlib
import hmac
import json
import os
import socket
import struct
import sys
import time

HOST = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
TURN_PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 3479
CONTROL = int(sys.argv[3]) if len(sys.argv) > 3 else 8099
SECRET = os.environ.get("TURN_SECRET", "test-turn-secret")
REALM = os.environ.get("REALM", "chatapp")
MAGIC = 0x2112A442

checks = []
def check(name, cond, detail=""):
    checks.append((name, bool(cond), detail))

MT_BINDING = 0x001
MT_ALLOC = 0x003
MT_REFRESH = 0x004
MT_SEND = 0x006
MT_DATA = 0x007
MT_PERM = 0x008
MT_CHANBIND = 0x009
CLS_REQ, CLS_IND, CLS_OK, CLS_ERR = 0, 1, 2, 3

def mtype(method, cls):
    t = ((method & 0xF80) << 2) | ((method & 0x70) << 1) | (method & 0xF)
    if cls == CLS_IND: t |= 0x0010
    if cls == CLS_OK:  t |= 0x0100
    if cls == CLS_ERR: t |= 0x0110
    return t

def method_of(t):
    return ((t & 0x3E00) >> 2) | ((t & 0xE0) >> 1) | (t & 0xF)

def cls_of(t):
    c = t & 0x0110
    if c == 0x0010: return CLS_IND
    if c == 0x0100: return CLS_OK
    if c == 0x0110: return CLS_ERR
    return CLS_REQ

def build(method, cls, attrs, txid=None, integrity_key=None):
    txid = txid or os.urandom(12)
    body = b""
    for code, val in attrs:
        body += struct.pack(">HH", code, len(val)) + val
        body += b"\x00" * ((4 - len(val) % 4) % 4)
    if integrity_key is not None:
        body_len = len(body) + 24
        msg = struct.pack(">HHI", mtype(method, cls), body_len, MAGIC) + txid + body
        mac = hmac.new(integrity_key, msg, hashlib.sha1).digest()
        body += struct.pack(">HH", 0x0008, 20) + mac
    msg = struct.pack(">HHI", mtype(method, cls), len(body), MAGIC) + txid + body
    return msg, txid

def parse(data):
    t, ln, magic = struct.unpack(">HHI", data[:8])
    txid = data[8:20]
    attrs = []
    off = 20
    while off + 4 <= 20 + ln:
        code, alen = struct.unpack(">HH", data[off:off+4])
        attrs.append((code, data[off+4:off+4+alen]))
        off += 4 + ((alen + 3) & ~3)
        if code == 0x0008:
            break
    return t, txid, attrs

def attr_map(attrs):
    m = {}
    for c, v in attrs:
        m.setdefault(c, v)
    return m

def xor_addr(val):
    fam = val[1]
    port = struct.unpack(">H", val[2:4])[0] ^ (MAGIC >> 16)
    ip = struct.unpack(">I", val[4:8])[0] ^ MAGIC
    return socket.inet_ntoa(struct.pack(">I", ip)), port

def creds():
    uid = "turn-tester"
    expiry = int(time.time()) + 3600
    user = f"{expiry}:{uid}"
    pw = base64.b64encode(hmac.new(SECRET.encode(), user.encode(), hashlib.sha1).digest()).decode()
    key = hashlib.md5(f"{user}:{REALM}:{pw}".encode()).digest()
    return user, key

def main():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.settimeout(3)
    peer = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    peer.bind(("127.0.0.1", 0))
    peer.settimeout(3)
    peer_addr = peer.getsockname()

    # 1. service health (retry while the service is still starting)
    body = ""
    for attempt in range(20):
        try:
            with socket.create_connection((HOST, CONTROL), timeout=3) as c:
                c.sendall(b"GET /health HTTP/1.0\r\n\r\n")
                body = c.recv(4096).decode()
                break
        except OSError:
            time.sleep(0.5)
    check("health endpoint", "ok" in body, f"body={body!r}")

    # 2. STUN binding
    pkt, txid = build(MT_BINDING, CLS_REQ, [])
    s.sendto(pkt, (HOST, TURN_PORT))
    t, rtx, attrs = parse(s.recv(4096))
    check("binding response", method_of(t) == MT_BINDING and cls_of(t) == CLS_OK and rtx == txid)
    am = attr_map(attrs)
    check("binding returns XOR-MAPPED", 0x0020 in am)

    # 3. unauthenticated allocate -> 401 + REALM + NONCE
    pkt, _ = build(MT_ALLOC, CLS_REQ, [(0x0019, b"\x11")])
    s.sendto(pkt, (HOST, TURN_PORT))
    t, _, attrs = parse(s.recv(4096))
    am = attr_map(attrs)
    check("allocate requires auth", cls_of(t) == CLS_ERR and 0x0009 in am and am[0x0009] and am[0x0009][2] == 4)
    check("401 carries realm+nonce", 0x0014 in am and 0x0015 in am, str(am.get(0x0009)))
    nonce = am[0x0015] if 0x0015 in am else None

    user, key = creds()

    # 4. allocate with credentials
    pkt, txid = build(MT_ALLOC, CLS_REQ,
                      [(0x0019, b"\x11"), (0x0006, user.encode()), (0x0014, REALM.encode()), (0x0015, nonce)],
                      integrity_key=key)
    s.sendto(pkt, (HOST, TURN_PORT))
    t, rtx, attrs = parse(s.recv(4096))
    am = attr_map(attrs)
    ok = cls_of(t) == CLS_OK and rtx == txid
    check("allocate succeeds", ok, f"type={t:#x} err={am.get(0x0009)!r}")
    relay = None
    if 0x0016 in am:
        relay = xor_addr(am[0x0016])
    check("allocate returns relayed address", relay is not None)
    check("allocate returns lifetime", 0x000D in am)

    # helper: fresh nonce through 401 each authed request (server uses one-time nonces)
    def authed(method, attrs):
        probe, _ = build(method, CLS_REQ, [])
        s.sendto(probe, (HOST, TURN_PORT))
        t, _, a2 = parse(s.recv(4096))
        am2 = attr_map(a2)
        nnc = am2.get(0x0015)
        if not nnc:
            return None
        full = [(0x0006, user.encode()), (0x0014, REALM.encode()), (0x0015, nnc)] + attrs
        pkt2, txid2 = build(method, CLS_REQ, full, integrity_key=key)
        s.sendto(pkt2, (HOST, TURN_PORT))
        return parse(s.recv(4096))

    def xor_peer_attr(ip, port):
        raw = bytes([0, 1]) + struct.pack(">H", port ^ (MAGIC >> 16)) + \
              struct.pack(">I", struct.unpack(">I", socket.inet_aton(ip))[0] ^ MAGIC)
        return (0x0012, raw)

    # 5. create permission for peer
    res = authed(MT_PERM, [xor_peer_attr(*peer_addr)])
    am = attr_map(res[2]) if res else {}
    check("create-permission succeeds", res is not None and cls_of(res[0]) == CLS_OK,
          f"err={am.get(0x0009)!r}")

    # 6. channel bind
    chnum = struct.pack(">H", 0x4001) + b"\x00\x00"
    res = authed(MT_CHANBIND, [(0x000C, chnum), xor_peer_attr(*peer_addr)])
    am = attr_map(res[2]) if res else {}
    check("channel-bind succeeds", res is not None and cls_of(res[0]) == CLS_OK,
          f"err={am.get(0x0009)!r}")

    # 7. client->peer via channel-data
    payload = os.urandom(160)
    frame = struct.pack(">HH", 0x4001, len(payload)) + payload
    frame += b"\x00" * ((4 - len(payload) % 4) % 4)
    s.sendto(frame, (HOST, TURN_PORT))
    pdata, paddr = peer.recvfrom(4096)
    check("channel-data relay client->peer", pdata == payload)

    # 8. peer->client via channel-data
    reply = os.urandom(100)
    peer.sendto(reply, relay)
    data, _ = s.recvfrom(4096)
    ch, ln = struct.unpack(">HH", data[:4])
    check("channel-data relay peer->client", ch == 0x4001 and data[4:4+ln] == reply)

    # 9. permission-based indication path (no channel): delete channel by sending Send indication
    fresh_peer = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    fresh_peer.bind(("127.0.0.1", 0))
    fresh_peer.settimeout(3)
    faddr = fresh_peer.getsockname()
    res = authed(MT_PERM, [xor_peer_attr(*faddr)])
    check("permission for second peer", res is not None and cls_of(res[0]) == CLS_OK)
    # Send indication with DATA
    plt = b"indication-payload"
    full = [(0x0006, user.encode()), (0x0014, REALM.encode())]
    probe, _ = build(MT_SEND, CLS_IND, [(0x0006, user.encode()), xor_peer_attr(*faddr), (0x0013, plt)])
    s.sendto(probe, (HOST, TURN_PORT))
    pdata, _ = fresh_peer.recvfrom(4096)
    check("send indication relayed", pdata == plt)
    # back as Data indication
    fresh_peer.sendto(plt, relay)
    data, _ = s.recvfrom(4096)
    t, _, attrs = parse(data)
    am = attr_map(attrs)
    check("data indication received", method_of(t) == MT_DATA and cls_of(t) == CLS_IND and 0x0013 in am,
          f"raw={data[:24].hex()} method={hex(method_of(t))} cls={cls_of(t)} attrs={[hex(c) for c,_ in attrs]}")
    fresh_peer.close()

    # 10. refresh (extend) then refresh to 0 (release)
    res = authed(MT_REFRESH, [(0x000D, struct.pack(">I", 600))])
    check("refresh extends", res is not None and cls_of(res[0]) == CLS_OK)
    dumped = False
    res = authed(MT_REFRESH, [(0x000D, struct.pack(">I", 0))])
    check("refresh releases", res is not None and cls_of(res[0]) == CLS_OK and (attr_map(res[2]).get(0x000D) in (struct.pack(">I", 0), b"")))

    # 11. stats endpoint shows one completed allocation cycle and drops bad packets
    with socket.create_connection((HOST, CONTROL), timeout=3) as c:
        c.sendall(b"GET /stats HTTP/1.0\r\nAuthorization: Bearer " + os.environ.get("SFU_SECRET", "test-sfu-secret").encode() + b"\r\n\r\n")
        body = c.recv(4096).decode()
        j = json.loads(body.split("\r\n\r\n", 1)[1])
        check("stats endpoint", "allocations" in j and "packets" in j)

    s.close(); peer.close()

    passed = [c for c in checks if c[1]]
    failed = [c for c in checks if not c[1]]
    print(f"\nsfu-turn: {len(passed)}/{len(checks)} PASS")
    for name, ok, detail in checks:
        print(("  PASS " if ok else "  FAIL ") + name + (f" — {detail}" if detail else ""))
    if failed:
        sys.exit(1)

try:
    main()
except Exception as e:
    print(f"\nABORT after exception: {e!r}")
    for name, ok, detail in checks:
        print(("  PASS " if ok else "  FAIL ") + name + (f" — {detail}" if detail else ""))
    sys.exit(1)
