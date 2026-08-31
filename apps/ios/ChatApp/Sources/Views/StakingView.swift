import SwiftUI

struct StakingAssetItem: Identifiable {
    let id = UUID()
    let asset: String
    let chain: String
    let apy: String
    let durations: [Int]
    let minAmount: String
    let maxAmount: String
    let priceUsd: String?
}

struct StakingPositionItem: Identifiable {
    let id: String
    let asset: String
    let chain: String
    let amount: String
    let apy: String
    let durationDays: Int
    let endsAt: String
    let status: String
    let reward: String?
    let accrued: String?
}

struct StakingView: View {
    @EnvironmentObject var session: SessionStore
    @State private var assets: [StakingAssetItem] = []
    @State private var positions: [StakingPositionItem] = []
    @State private var amountByAsset: [String: String] = [:]
    @State private var message: String?
    @State private var error: String?

    private func client() -> APIClient { APIClient(token: session.accessToken) }

    func load() {
        Task {
            do {
                let aData = try await client().get("/api/staking/assets")
                let aObj = try JSONSerialization.jsonObject(with: aData) as? [String: Any]
                assets = (aObj?["assets"] as? [[String: Any]] ?? []).map {
                    StakingAssetItem(
                        asset: $0["asset"] as? String ?? "",
                        chain: $0["chain"] as? String ?? "",
                        apy: $0["apy"] as? String ?? "",
                        durations: $0["durations"] as? [Int] ?? [],
                        minAmount: $0["min_amount"] as? String ?? "",
                        maxAmount: $0["max_amount"] as? String ?? "",
                        priceUsd: $0["price_usd"] as? String)
                }
                let pData = try await client().get("/api/staking/positions")
                let pObj = try JSONSerialization.jsonObject(with: pData) as? [String: Any]
                positions = (pObj?["positions"] as? [[String: Any]] ?? []).map {
                    StakingPositionItem(
                        id: $0["id"] as? String ?? UUID().uuidString,
                        asset: $0["asset"] as? String ?? "",
                        chain: $0["chain"] as? String ?? "",
                        amount: $0["amount"] as? String ?? "",
                        apy: $0["apy"] as? String ?? "",
                        durationDays: $0["duration_days"] as? Int ?? 0,
                        endsAt: $0["ends_at"] as? String ?? "",
                        status: $0["status"] as? String ?? "",
                        reward: $0["reward"] as? String,
                        accrued: $0["accrued_estimate"] as? String)
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    func openPosition(_ asset: StakingAssetItem, duration: Int) {
        let key = "\(asset.asset)/\(asset.chain)"
        let amount = amountByAsset[key] ?? ""
        error = nil
        message = nil
        Task {
            do {
                let body: [String: Any] = ["asset": asset.asset, "chain": asset.chain,
                                           "amount": amount, "duration_days": duration]
                _ = try await client().post("/api/staking/positions", body: body)
                message = "Position opened"
                amountByAsset[key] = ""
                load()
            } catch { self.error = error.localizedDescription }
        }
    }

    func requestUnlock(_ pos: StakingPositionItem) {
        error = nil
        message = nil
        Task {
            do {
                let data = try await client().post("/api/staking/positions/\(pos.id)/unlock")
                let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                message = obj?["message"] as? String ?? "Unlock queued"
                load()
            } catch { self.error = error.localizedDescription }
        }
    }

    var body: some View {
        List {
            Section("Assets") {
                ForEach(assets) { a in
                    let key = "\(a.asset)/\(a.chain)"
                    VStack(alignment: .leading, spacing: 6) {
                        Text("\(a.asset) (\(a.chain)) · APY \(a.apy)")
                            .font(.headline)
                        Text("durations: \(a.durations.map(String.init).joined(separator: ", ")) · min \(a.minAmount) · max \(a.maxAmount)"
                            + (a.priceUsd?.count ?? 0 > 0 ? " · live $\(a.priceUsd ?? "")" : ""))
                            .font(.caption)
                            .foregroundColor(.secondary)
                        HStack {
                            TextField("amount", text: Binding(
                                get: { amountByAsset[key] ?? "" },
                                set: { amountByAsset[key] = $0 }))
                                .textFieldStyle(.roundedBorder)
                            Button("Stake") { openPosition(a, duration: a.durations.first ?? 7) }
                        }
                    }
                }
                if assets.isEmpty { Text("No staking assets").foregroundColor(.secondary) }
            }
            Section("Your positions") {
                ForEach(positions) { p in
                    VStack(alignment: .leading, spacing: 4) {
                        Text("\(p.asset)/\(p.chain) · \(p.amount)").font(.headline)
                        Text("APY \(p.apy) · \(p.durationDays)d · matures \(p.endsAt.prefix(10)) · \(p.status)")
                            .font(.caption).foregroundColor(.secondary)
                        if let r = p.accrued ?? p.reward { Text("reward: \(r)").font(.caption) }
                        if p.status == "active" {
                            Button("Unlock") { requestUnlock(p) }
                        } else if p.status == "unlock_requested" {
                            Text("Queued for settlement").font(.caption).foregroundColor(.secondary)
                        }
                    }
                }
                if positions.isEmpty { Text("No positions yet").foregroundColor(.secondary) }
            }
            if let msg = message { Section { Text(msg).foregroundColor(.green) } }
            if let err = error { Section { Text(err).foregroundColor(.red) } }
        }
        .navigationTitle("Staking")
        .task { load() }
    }
}
