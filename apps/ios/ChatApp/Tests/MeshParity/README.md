# iOS mesh parity tests

JVM/Linux parity harness for the iOS mesh engine, mirroring the Android
`MeshParityTest.kt` and the Go `services/mesh` test suite.

## Run

```bash
git clone --depth 1 --branch 3.12.3 https://github.com/apple/swift-crypto /tmp/swift-crypto
cd /tmp/swift-crypto && swift build -c debug -Xswiftc -DCRYPTO_IN_SWIFTPM_FORCE_BUILD_API

mkdir -p /tmp/secshim
# Security shim (CryptoKit is Apple-only; on Linux swift-crypto provides the primitives)
cp Tests/MeshParity/SecurityShim.swift /tmp/secshim/Security.swift
cd /tmp/secshim && /opt/swift*/usr/bin/swiftc -emit-module -module-name Security \
  -emit-module-path /tmp/secshim/Security.swiftmodule Security.swift

/opt/swift*/usr/bin/swiftc -parse-as-library \
  -I /tmp/swift-crypto/.build/x86_64-unknown-linux-gnu/debug/Modules -I /tmp/secshim -lc++ \
  -o /tmp/swifttest \
  /tmp/secshim/Security.o \
  /tmp/swift-crypto/.build/x86_64-unknown-linux-gnu/debug/Modules/Crypto.o \
  /tmp/swift-crypto/.build/x86_64-unknown-linux-gnu/debug/Modules/CryptoBoringWrapper.o \
  Tests/MeshParity/MeshLinkStub.swift \
  Sources/Services/MeshIdentity.swift Sources/Services/MeshEngine.swift \
  Sources/Services/MeshFragments.swift Sources/Services/MeshGroups.swift \
  Sources/Services/MeshReliability.swift Sources/Services/MeshRevocation.swift \
  Tests/MeshParity/MeshParityMain.swift

/tmp/swifttest
```

`MeshLinkStub.swift` stands in for the app's `MeshLink` protocol from
`MeshTransport.swift` (which imports UIKit/CoreBluetooth). The seven cases
cover: reliable unicast + ack settle, fragmentation reassembly, group keys
adoption + per-member group acks, network-wide revocation distribution, and
priority-queue ordering.
