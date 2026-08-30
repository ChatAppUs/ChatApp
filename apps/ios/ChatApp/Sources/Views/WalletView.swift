import SwiftUI
import AVFoundation
import CoreImage.CIFilterBuiltins
import UIKit

// Wallet: balances, deterministic multi-chain deposit addresses (QR + copy),
// signed withdrawals (paste or camera-scan the destination QR), convert and
// the escrowed P2P market — same feature set as web/desktop/Android.

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
}

struct P2PTradeItem: Identifiable {
    let id: String
    let asset: String
    let amount: String
    let fiat: String
    let status: String
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
    @State private var scanning = false
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
                                 methods: ($0["payment_methods"] as? [String] ?? []).joined(separator: ", "))
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
                }
                .pickerStyle(.segmented)
                .padding(.horizontal)
                .onChange(of: tab) { _, newTab in
                    if newTab == "p2p" { loadOffers() }
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
                default:
                    List {
                        Section("Offers") {
                            ForEach(offers) { o in
                                VStack(alignment: .leading) {
                                    Text("\(o.side.uppercased()) \(o.asset) @ \(o.price) \(o.fiat) — @\(o.owner)")
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
