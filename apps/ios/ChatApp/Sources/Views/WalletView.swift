import SwiftUI
import AVFoundation
import CoreImage.CIFilterBuiltins
import UIKit

// Wallet: balances, deterministic multi-chain deposit addresses (QR + copy),
// signed withdrawals (paste or camera-scan the destination QR), convert and
// the escrowed P2P market — same feature set as web/desktop/Android.

struct PriceItem: Identifiable {
    let id = UUID()
    let asset: String
    let chain: String
    let usd: String?
    let source: String?
    let fetchedAt: String?
}

struct WalletAccountItem: Identifiable {
    let id: String
    let asset: String
    let chain: String
    let balance: String
}

struct P2POfferItem: Identifiable {
    let id: String
    let owner: String
    let side: String
    let asset: String
    let price: String
    let fiat: String
    let methods: String
    let ownerIsMerchant: Bool
    let ownerMerchantTier: Int
}

struct P2PTradeItem: Identifiable {
    let id: String
    let asset: String
    let amount: String
    let fiat: String
    let status: String
}

struct MerchantInfo {
    let status: String
    let tier: Int
    let tierName: String
    let maxTradeUsd: String
    let dailyVolumeUsd: String
}

struct CardItem: Identifiable {
    let id: String
    let label: String
    let last4: String
    let status: String
    let balanceUsd: String
    let dailyLimitUsd: String
    let monthlyLimitUsd: String
}

struct CardTxnItem: Identifiable {
    let id: String
    let amountUsd: String
    let merchant: String
    let status: String
    let createdAt: String
}

private func qrImage(_ content: String) -> UIImage? {
    let filter = CIFilter.qrCodeGenerator()
    filter.message = Data(content.utf8)
    filter.correctionLevel = "M"
    guard let output = filter.outputImage else { return nil }
    let scaled = output.transformed(by: CGAffineTransform(scaleX: 8, y: 8))
    return UIImage(ciImage: scaled)
}

private func parseAddressPayload(_ raw: String) -> String {
    let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    if let range = trimmed.range(of: #"^(?:bitcoin|ethereum|tron|solana|litecoin|dogecoin):"#,
                                 options: .regularExpression) {
        let rest = trimmed[range.upperBound...]
        if let amp = rest.firstIndex(of: "?") {
            return String(rest[..<amp])
        }
        return String(rest)
    }
    return trimmed
}

struct WalletView: View {
    @EnvironmentObject var session: SessionStore
    @State private var tab = "wallet"
    @State private var accounts: [WalletAccountItem] = []
    @State private var asset = "USDT"
    @State private var chain = "tron"
    @State private var depositAddress = ""
    @State private var depositURI = ""
    @State private var withdrawTo = ""
    @State private var withdrawAmount = ""
    @State private var convertAmount = ""
    @State private var offers: [P2POfferItem] = []
    @State private var trades: [P2PTradeItem] = []
    @State private var merchant: MerchantInfo?
    @State private var merchantNote = ""
    @State private var cards: [CardItem] = []
    @State private var cardTxns: [CardTxnItem] = []
    @State private var cardLabel = ""
    @State private var topupAmount = ""
    @State private var issuedCardDetails = ""
    @State private var scanning = false
    @State private var prices: [PriceItem] = []
    @State private var message: String?
    @State private var error: String?

    private func client() -> APIClient { APIClient(token: session.accessToken) }

    func load() {
        Task {
            do {
                let data = try await client().get("/api/wallet/accounts")
                let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                accounts = (obj?["accounts"] as? [[String: Any]] ?? []).map {
                    WalletAccountItem(id: $0["id"] as? String ?? "",
                                      asset: $0["asset"] as? String ?? "",
                                      chain: $0["chain"] as? String ?? "",
                                      balance: $0["balance"] as? String ?? "0")
                }
                let tdata = try await client().get("/api/p2p/trades")
                let tobj = try JSONSerialization.jsonObject(with: tdata) as? [String: Any]
                trades = (tobj?["trades"] as? [[String: Any]] ?? []).map {
                    P2PTradeItem(id: $0["id"] as? String ?? "",
                                 asset: $0["asset"] as? String ?? "",
                                 amount: $0["crypto_amount"] as? String ?? "",
                                 fiat: "\($0["fiat_amount"] as? String ?? "") \($0["fiat_currency"] as? String ?? "")",
                                 status: $0["status"] as? String ?? "")
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    func loadOffers() {
        Task {
            do {
                let data = try await client().get("/api/p2p/offers")
                let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                offers = (obj?["offers"] as? [[String: Any]] ?? []).map {
                    P2POfferItem(id: $0["id"] as? String ?? "",
                                 owner: $0["owner_username"] as? String ?? "",
                                 side: $0["side"] as? String ?? "",
                                 asset: $0["asset"] as? String ?? "",
                                 price: $0["price"] as? String ?? "",
                                 fiat: $0["fiat_currency"] as? String ?? "",
                                 methods: ($0["payment_methods"] as? [String] ?? []).joined(separator: ", "),
                                 ownerIsMerchant: $0["owner_is_merchant"] as? Bool ?? false,
                                 ownerMerchantTier: $0["owner_merchant_tier"] as? Int ?? 0)
                }
                if let mdata = try? await client().get("/api/p2p/merchant/status"),
                   let mobj = try JSONSerialization.jsonObject(with: mdata) as? [String: Any],
                   let m = mobj["merchant"] as? [String: Any] {
                    merchant = MerchantInfo(
                        status: m["status"] as? String ?? "",
                        tier: m["tier"] as? Int ?? 0,
                        tierName: m["tier_name"] as? String ?? "",
                        maxTradeUsd: mobj["max_trade_usd"] as? String ?? "",
                        dailyVolumeUsd: mobj["daily_volume_usd"] as? String ?? "")
                } else {
                    merchant = nil
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    func loadCards() {
        Task {
            do {
                let data = try await client().get("/api/cards")
                let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                cards = (obj?["cards"] as? [[String: Any]] ?? []).map {
                    CardItem(id: $0["id"] as? String ?? "",
                             label: $0["label"] as? String ?? "",
                             last4: $0["last4"] as? String ?? "",
                             status: $0["status"] as? String ?? "",
                             balanceUsd: $0["balance_usd"] as? String ?? "0",
                             dailyLimitUsd: $0["daily_limit_usd"] as? String ?? "",
                             monthlyLimitUsd: $0["monthly_limit_usd"] as? String ?? "")
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    func loadCardTxns(_ cardId: String) {
        Task {
            do {
                let data = try await client().get("/api/cards/\(cardId)/transactions")
                let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                cardTxns = (obj?["transactions"] as? [[String: Any]] ?? []).map {
                    CardTxnItem(id: $0["id"] as? String ?? "",
                                amountUsd: $0["amount_usd"] as? String ?? "",
                                merchant: $0["merchant"] as? String ?? "",
                                status: $0["status"] as? String ?? "",
                                createdAt: $0["created_at"] as? String ?? "")
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    var body: some View {
        NavigationStack {
            VStack {
                Picker("Section", selection: $tab) {
                    Text("Wallet").tag("wallet")
                    Text("Deposit").tag("deposit")
                    Text("Withdraw").tag("withdraw")
                    Text("Convert").tag("convert")
                    Text("P2P").tag("p2p")
                    Text("Cards").tag("cards")
                    Text("Prices").tag("prices")
                }
                .pickerStyle(.segmented)
                .padding(.horizontal)
                .onChange(of: tab) { _, newTab in
                    if newTab == "p2p" { loadOffers() }
                    if newTab == "cards" { loadCards() }
                    if newTab == "prices" { loadPrices() }
                }
                if let message { Text(message).foregroundStyle(.green).font(.footnote) }
                if let error { Text(error).foregroundStyle(.red).font(.footnote) }

                switch tab {
                case "wallet":
                    List(accounts) { a in
                        HStack {
                            Text("\(a.asset) · \(a.chain)")
                            Spacer()
                            Text(a.balance)
                        }
                    }
                case "deposit":
                    Form {
                        TextField("Asset (e.g. USDT)", text: $asset)
                        TextField("Chain (e.g. tron)", text: $chain)
                        Button("Get deposit address") {
                            Task {
                                do {
                                    let data = try await client().post("/api/wallet/deposit-address",
                                        body: ["asset": asset.uppercased(), "chain": chain.lowercased()])
                                    let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                                    depositAddress = obj?["address"] as? String ?? ""
                                    depositURI = obj?["uri"] as? String ?? depositAddress
                                } catch { self.error = error.localizedDescription }
                            }
                        }
                        if !depositAddress.isEmpty {
                            if let img = qrImage(depositURI) {
                                Image(uiImage: img)
                                    .interpolation(.none)
                                    .resizable()
                                    .frame(width: 180, height: 180)
                            }
                            Text(depositAddress).font(.footnote).textSelection(.enabled)
                            Button("Copy address") {
                                UIPasteboard.general.string = depositAddress
                                message = "Copied"
                            }
                        }
                    }
                case "withdraw":
                    Form {
                        TextField("Destination address", text: $withdrawTo)
                            .textInputAutocapitalization(.never)
                        HStack {
                            Button("Paste") {
                                if let text = UIPasteboard.general.string {
                                    withdrawTo = parseAddressPayload(text)
                                }
                            }
                            Button(scanning ? "Stop scan" : "Scan QR") { scanning.toggle() }
                        }
                        if scanning {
                            QRScannerRepresentable { code in
                                withdrawTo = parseAddressPayload(code)
                                scanning = false
                            }
                            .frame(height: 240)
                        }
                        TextField("Amount", text: $withdrawAmount).keyboardType(.decimalPad)
                        Button("Withdraw") {
                            Task {
                                do {
                                    let data = try await client().post("/api/wallet/withdraw", body: [
                                        "asset": asset.uppercased(), "chain": chain.lowercased(),
                                        "to_address": withdrawTo, "amount": withdrawAmount,
                                    ])
                                    let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                                    let status = obj?["status"] as? String ?? ""
                                    let ms = obj?["approved_in_ms"] as? Int ?? 0
                                    let auto = obj?["auto_approved"] as? Bool ?? false
                                    message = "Withdrawal \(status)" + (auto ? " — auto-approved in \(ms)ms" : "")
                                    withdrawTo = ""; withdrawAmount = ""
                                    load()
                                } catch { self.error = error.localizedDescription }
                            }
                        }
                    }
                case "convert":
                    Form {
                        TextField("Amount", text: $convertAmount).keyboardType(.decimalPad)
                        Button("Convert \(asset) to USD") {
                            Task {
                                do {
                                    let data = try await client().post("/api/convert", body: [
                                        "from_asset": asset.uppercased(), "from_chain": chain.lowercased(),
                                        "to_asset": "USD", "to_chain": "internal",
                                        "amount": convertAmount,
                                    ])
                                    let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                                    message = "Received \(obj?["to_amount"] as? String ?? "") USD"
                                    convertAmount = ""
                                    load()
                                } catch { self.error = error.localizedDescription }
                            }
                        }
                    }
                case "prices":
                    List {
                        Section("Live prices") {
                            ForEach(prices) { p in
                                HStack {
                                    Text("\(p.asset) (\(p.chain))")
                                    Spacer()
                                    Text("\(p.usd ?? "—") USD")
                                    if let s = p.source { Text(s).font(.caption2).foregroundStyle(.secondary) }
                                }
                            }
                            if prices.isEmpty { Text("No prices yet").foregroundStyle(.secondary) }
                        }
                    }
                case "cards":
                    List {
                        Section("Issue virtual card") {
                            TextField("Label (optional)", text: $cardLabel)
                            Button("Issue card") {
                                Task {
                                    do {
                                        let data = try await client().post("/api/cards",
                                            body: ["label": cardLabel])
                                        let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                                        let pan = obj?["card_number"] as? String ?? ""
                                        let cvv = obj?["cvv"] as? String ?? ""
                                        issuedCardDetails = pan.isEmpty ? "" : "\(pan)  CVV \(cvv)"
                                        message = "Card issued — details shown once, store safely"
                                        loadCards()
                                    } catch { self.error = error.localizedDescription }
                                }
                            }
                            if !issuedCardDetails.isEmpty {
                                Text(issuedCardDetails).font(.footnote).textSelection(.enabled)
                            }
                        }
                        Section("My cards") {
                            ForEach(cards) { c in
                                VStack(alignment: .leading) {
                                    Text("\(c.label.isEmpty ? "Card" : c.label) ···· \(c.last4) [\(c.status)]")
                                    Text("Balance $\(c.balanceUsd) · limits $\(c.dailyLimitUsd)/d $\(c.monthlyLimitUsd)/m")
                                        .font(.footnote).foregroundStyle(.secondary)
                                    HStack {
                                        if c.status == "active" {
                                            Button("Freeze") { setCardStatus(c.id, "frozen") }
                                        }
                                        if c.status == "frozen" {
                                            Button("Unfreeze") { setCardStatus(c.id, "active") }
                                        }
                                        Button("Statement") { loadCardTxns(c.id) }
                                    }
                                    HStack {
                                        TextField("Amount \(asset)", text: $topupAmount)
                                            .keyboardType(.decimalPad)
                                        Button("Top up") {
                                            Task {
                                                do {
                                                    _ = try await client().post("/api/cards/\(c.id)/topup", body: [
                                                        "asset": asset.uppercased(),
                                                        "chain": chain.lowercased(),
                                                        "amount": topupAmount,
                                                    ])
                                                    message = "Card topped up"
                                                    topupAmount = ""
                                                    loadCards(); load()
                                                } catch { self.error = error.localizedDescription }
                                            }
                                        }
                                    }
                                }
                            }
                        }
                        if !cardTxns.isEmpty {
                            Section("Transactions") {
                                ForEach(cardTxns) { t in
                                    VStack(alignment: .leading) {
                                        Text("$\(t.amountUsd) · \(t.merchant) [\(t.status)]")
                                        Text(t.createdAt).font(.footnote).foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                default:
                    List {
                        Section("Merchant program") {
                            if let m = merchant {
                                Text("Status: \(m.status)" +
                                     (m.status == "verified" ? " · Tier \(m.tier) \(m.tierName)" : ""))
                                if m.status == "verified" {
                                    Text("Limits: $\(m.maxTradeUsd)/trade · $\(m.dailyVolumeUsd)/day")
                                        .font(.footnote).foregroundStyle(.secondary)
                                }
                            } else {
                                Text("Become a verified merchant for a badge and higher limits.")
                                    .font(.footnote).foregroundStyle(.secondary)
                                TextField("Business description", text: $merchantNote)
                                Button("Apply") {
                                    Task {
                                        do {
                                            _ = try await client().post("/api/p2p/merchant/apply",
                                                body: ["note": merchantNote])
                                            message = "Application submitted"
                                            loadOffers()
                                        } catch { self.error = error.localizedDescription }
                                    }
                                }
                            }
                        }
                        Section("Offers") {
                            ForEach(offers) { o in
                                VStack(alignment: .leading) {
                                    Text("\(o.side.uppercased()) \(o.asset) @ \(o.price) \(o.fiat) — @\(o.owner)" +
                                         (o.ownerIsMerchant ? " 🏪T\(o.ownerMerchantTier)" : ""))
                                    Text(o.methods).font(.footnote).foregroundStyle(.secondary)
                                }
                            }
                        }
                        Section("My trades") {
                            ForEach(trades) { tr in
                                VStack(alignment: .leading) {
                                    Text("\(tr.amount) \(tr.asset) ⇄ \(tr.fiat) [\(tr.status)]")
                                    HStack {
                                        if tr.status == "open" {
                                            Button("Paid") { tradeAction(tr.id, "paid") }
                                            Button("Cancel") { tradeAction(tr.id, "cancel") }
                                        }
                                        if tr.status == "paid" {
                                            Button("Release") { tradeAction(tr.id, "release") }
                                            Button("Dispute") { tradeAction(tr.id, "dispute") }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Wallet")
            .onAppear(perform: load)
        }
    }

    private func tradeAction(_ id: String, _ action: String) {
        Task {
            do {
                _ = try await client().post("/api/p2p/trades/\(id)/\(action)")
                load()
            } catch { self.error = error.localizedDescription }
        }
    }

    private func loadPrices() {
        Task {
            do {
                let data = try await client().get("/api/prices")
                let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any]
                prices = (obj?["prices"] as? [[String: Any]] ?? []).map {
                    PriceItem(asset: $0["asset"] as? String ?? "",
                              chain: $0["chain"] as? String ?? "",
                              usd: $0["usd"] as? String,
                              source: $0["source"] as? String,
                              fetchedAt: $0["fetched_at"] as? String)
                }
            } catch { self.error = error.localizedDescription }
        }
    }

    private func setCardStatus(_ id: String, _ status: String) {
        Task {
            do {
                _ = try await client().post("/api/cards/\(id)/status", body: ["status": status])
                loadCards()
            } catch { self.error = error.localizedDescription }
        }
    }
}

// AVFoundation QR scanner for withdrawal addresses (no third-party deps).
struct QRScannerRepresentable: UIViewControllerRepresentable {
    let onResult: (String) -> Void

    func makeUIViewController(context: Context) -> QRScannerViewController {
        let vc = QRScannerViewController()
        vc.onResult = onResult
        return vc
    }

    func updateUIViewController(_ uiViewController: QRScannerViewController, context: Context) {}
}

final class QRScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onResult: ((String) -> Void)?
    private let session = AVCaptureSession()

    override func viewDidLoad() {
        super.viewDidLoad()
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else { return }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.frame = view.bounds
        preview.videoGravity = .resizeAspectFill
        view.layer.addSublayer(preview)
        DispatchQueue.global(qos: .userInitiated).async { self.session.startRunning() }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        view.layer.sublayers?.first?.frame = view.bounds
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput,
                        didOutput metadataObjects: [AVMetadataObject],
                        from connection: AVCaptureConnection) {
        guard let obj = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let value = obj.stringValue else { return }
        session.stopRunning()
        onResult?(value)
    }
}
