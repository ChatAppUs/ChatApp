import SwiftUI

// PlatformViews.swift — iOS screens for the six platform features added in the
// gap-closure pass: Forums, Pulse, Live Shopping, AI Studio, AI assistant and
// the offline mesh status surface. They drive the same Go API endpoints as the
// web pages, so behaviour and data are identical across clients.

// MARK: - Models

struct Forum: Decodable, Identifiable {
    let id: String
    let title: String
    let slug: String?
    let description: String?
    let visibility: String
    let topic_count: Int
    let post_count: Int
}

struct ForumTopic: Decodable, Identifiable {
    let id: String
    let title: String
    let body: String?
    let author: String?
    let pinned: Bool
    let locked: Bool
    let post_count: Int
}

struct PulsePost: Decodable, Identifiable {
    let id: String
    let author: String?
    let body: String
    let topics: [String]?
    let reply_count: Int
    let repost_count: Int
    let quote_count: Int
}

struct PulseTrend: Decodable { let name: String }

struct ShopRoom: Decodable, Identifiable {
    let id: String
    let title: String
    let status: String
    let viewer_count: Int?
}

struct ShopProduct: Decodable, Identifiable {
    let id: String
    let name: String
    let price_cents: Int
    let stock: Int
}

struct DubJob: Decodable, Identifiable {
    let id: String
    let available: Bool
    let reason: String?
    let target_lang: String?
    let audio_url: String?
}

struct ClipJob: Decodable, Identifiable {
    let id: String
    let available: Bool
    let reason: String?
    let approved: Bool?
    let clips: [Clip]?
    struct Clip: Decodable { let title: String?; let score: Double? }
}

struct AssistantConversation: Decodable, Identifiable { let id: String }
struct AssistantAction: Decodable, Identifiable {
    let id: String
    let kind: String
    let status: String
}

// MARK: - Client extension

extension FeatureClient {
    private struct ForumList: Decodable { let forums: [Forum] }
    private struct TopicList: Decodable { let topics: [ForumTopic] }
    private struct PulseFeed: Decodable { let posts: [PulsePost] }
    private struct TrendList: Decodable { let trends: [PulseTrend] }
    private struct RoomList: Decodable { let rooms: [ShopRoom] }
    private struct ProductList: Decodable { let products: [ShopProduct] }
    private struct DubList: Decodable { let dubs: [DubJob] }
    private struct JobList: Decodable { let jobs: [ClipJob] }
    private struct ConvList: Decodable { let conversations: [AssistantConversation] }
    private struct ActionList: Decodable { let actions: [AssistantAction] }
    struct AssistantReply: Decodable { let reply: String?; let reason: String?; let action_kind: String? }

    func forums() async throws -> [Forum] {
        FeatureClient.decoded(ForumList.self, from: try await api.get("/api/forums"))?.forums ?? []
    }

    func createForum(title: String, description: String) async throws {
        _ = try await api.post("/api/forums", body: ["title": title, "description": description])
    }

    func topics(forumId: String) async throws -> [ForumTopic] {
        FeatureClient.decoded(TopicList.self, from: try await api.get("/api/forums/\(forumId)/topics"))?.topics ?? []
    }

    func createTopic(forumId: String, title: String) async throws {
        _ = try await api.post("/api/forums/\(forumId)/topics", body: ["title": title, "body": ""])
    }

    func reply(topicId: String, body: String) async throws {
        _ = try await api.post("/api/forums/topics/\(topicId)/posts", body: ["body": body])
    }

    func pulseFeed(local: Bool) async throws -> [PulsePost] {
        let path = local ? "/api/pulse/posts?scope=local" : "/api/pulse/posts"
        return FeatureClient.decoded(PulseFeed.self, from: try await api.get(path))?.posts ?? []
    }

    func pulseTrends(local: Bool) async throws -> [PulseTrend] {
        let path = local ? "/api/pulse/trends?scope=local" : "/api/pulse/trends"
        return FeatureClient.decoded(TrendList.self, from: try await api.get(path))?.trends ?? []
    }

    func createPulse(body: String, replyTo: String? = nil, repostOf: String? = nil, quoteOf: String? = nil) async throws {
        var payload: [String: Any] = ["body": body]
        if let replyTo { payload["parent_id"] = replyTo }
        if let repostOf { payload["repost_of"] = repostOf }
        if let quoteOf { payload["quote_of"] = quoteOf }
        _ = try await api.post("/api/pulse/posts", body: payload)
    }

    func liveRooms() async throws -> [ShopRoom] {
        FeatureClient.decoded(RoomList.self, from: try await api.get("/api/live-rooms?limit=50"))?.rooms ?? []
    }

    func liveProducts(roomId: String) async throws -> [ShopProduct] {
        FeatureClient.decoded(ProductList.self, from: try await api.get("/api/live-rooms/\(roomId)/products"))?.products ?? []
    }

    func createProduct(roomId: String, name: String, priceCents: Int, stock: Int) async throws {
        _ = try await api.post("/api/live-rooms/\(roomId)/products", body: [
            "name": name, "price_cents": priceCents, "stock": stock,
        ])
    }

    func pinProduct(roomId: String, productId: String) async throws {
        _ = try await api.post("/api/live-rooms/\(roomId)/pin", body: ["product_id": productId])
    }

    func checkout(roomId: String, productId: String, coupon: String?) async throws {
        var payload: [String: Any] = ["product_id": productId, "quantity": 1]
        if let coupon, !coupon.isEmpty { payload["coupon_code"] = coupon }
        _ = try await api.post("/api/live-rooms/\(roomId)/checkout", body: payload)
    }

    func dubs() async throws -> [DubJob] {
        FeatureClient.decoded(DubList.self, from: try await api.get("/api/ai/dubs"))?.dubs ?? []
    }

    struct DubResult: Decodable { let available: Bool; let reason: String?; let audio_url: String? }

    func dub(mediaUrl: String, targetLang: String) async throws -> DubResult? {
        FeatureClient.decoded(DubResult.self, from: try await api.post("/api/ai/dub", body: [
            "media_url": mediaUrl, "target_lang": targetLang,
        ]))
    }

    struct ClipResult: Decodable { let available: Bool; let reason: String?; let clips: [ClipJob.Clip]? }

    func analyzeClips(mediaUrl: String) async throws -> ClipResult? {
        FeatureClient.decoded(ClipResult.self, from: try await api.post("/api/ai/clips/analyze", body: ["media_url": mediaUrl]))
    }

    func clipJobs() async throws -> [ClipJob] {
        FeatureClient.decoded(JobList.self, from: try await api.get("/api/ai/clips"))?.jobs ?? []
    }

    func approveClip(_ clipId: String, approve: Bool) async throws {
        _ = try await api.post("/api/ai/clips/\(clipId)/approve", body: ["approve": approve])
    }

    func conversations() async throws -> [AssistantConversation] {
        FeatureClient.decoded(ConvList.self, from: try await api.get("/api/assistant/conversations"))?.conversations ?? []
    }

    func createConversation(title: String) async throws -> String? {
        struct Created: Decodable { let id: String }
        return FeatureClient.decoded(Created.self, from: try await api.post("/api/assistant/conversations", body: ["title": title]))?.id
    }

    func ask(conversationId: String, body: String) async throws -> AssistantReply? {
        FeatureClient.decoded(AssistantReply.self, from: try await api.post("/api/assistant/conversations/\(conversationId)/messages", body: ["body": body]))
    }

    func assistantActions() async throws -> [AssistantAction] {
        FeatureClient.decoded(ActionList.self, from: try await api.get("/api/assistant/actions"))?.actions ?? []
    }

    func decideAction(_ id: String, approve: Bool) async throws {
        _ = try await api.post("/api/assistant/actions/\(id)/decide", body: ["approve": approve])
    }
}

// MARK: - Forums

struct ForumsView: View {
    @EnvironmentObject var session: SessionStore
    @State private var topics: [String: [ForumTopic]] = [:]
    @State private var expanded: Set<String> = []
    @State private var forums: [Forum] = []
    @State private var title = ""
    @State private var description = ""
    @State private var replyText = ""
    @State private var replyTopic: String?
    @State private var error: String?

    private var client: FeatureClient { FeatureClient(token: session.accessToken) }

    func load() {
        Task {
            do { forums = try await client.forums() } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("New forum") {
                    TextField("Title", text: $title)
                    TextField("Description", text: $description)
                    Button("Create") {
                        Task {
                            do {
                                try await client.createForum(title: title, description: description)
                                title = ""; description = ""; load()
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(title.isEmpty)
                }
                Section {
                    ForEach(forums) { f in
                        VStack(alignment: .leading) {
                            Text(f.title).font(.headline)
                            if let d = f.description { Text(d).font(.caption) }
                            Text("\(f.topic_count) topics · \(f.post_count) posts").font(.caption2)
                            Button(expanded.contains(f.id) ? "Hide topics" : "Topics") {
                                expanded.toggleMembership(f.id)
                                if expanded.contains(f.id) {
                                    Task {
                                        do { topics[f.id] = try await client.topics(forumId: f.id) }
                                        catch { self.error = errorMessage(error) }
                                    }
                                }
                            }
                            if expanded.contains(f.id) {
                                Button("New topic") {
                                    Task {
                                        do {
                                            try await client.createTopic(forumId: f.id, title: "New topic")
                                            topics[f.id] = try await client.topics(forumId: f.id)
                                        } catch { self.error = errorMessage(error) }
                                    }
                                }
                                ForEach(topics[f.id] ?? []) { t in
                                    VStack(alignment: .leading) {
                                        Text("\(t.pinned ? "📌 " : "")\(t.locked ? "🔒 " : "")\(t.title)").font(.subheadline)
                                        Text("by \(t.author ?? "unknown") · \(t.post_count) replies").font(.caption2)
                                        HStack {
                                            TextField("Reply", text: Binding(
                                                get: { replyTopic == t.id ? replyText : "" },
                                                set: { replyText = $0; replyTopic = t.id }
                                            ))
                                            Button("Send") {
                                                Task {
                                                    do {
                                                        try await client.reply(topicId: t.id, body: replyText)
                                                        replyText = ""
                                                        topics[f.id] = try await client.topics(forumId: f.id)
                                                    } catch { self.error = errorMessage(error) }
                                                }
                                            }.disabled(replyText.isEmpty || t.locked)
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Forums")
            .onAppear(perform: load)
        }
    }
}

// MARK: - Pulse

struct PulseView: View {
    @EnvironmentObject var session: SessionStore
    @State private var posts: [PulsePost] = []
    @State private var trends: [PulseTrend] = []
    @State private var body_ = ""
    @State private var localOnly = false
    @State private var replyTo: String?
    @State private var error: String?

    private var client: FeatureClient { FeatureClient(token: session.accessToken) }

    func load() {
        Task {
            do {
                posts = try await client.pulseFeed(local: localOnly)
                trends = try await client.pulseTrends(local: localOnly)
            } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Compose") {
                    TextField(replyTo == nil ? "What's happening?" : "Reply", text: $body_, axis: .vertical)
                    Toggle("Local feed", isOn: $localOnly)
                    Button("Post") {
                        Task {
                            do {
                                try await client.createPulse(body: body_, replyTo: replyTo)
                                body_ = ""; replyTo = nil; load()
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(body_.isEmpty)
                }
                if !trends.isEmpty {
                    Section("Trends") {
                        Text(trends.prefix(6).map { "#\($0.name)" }.joined(separator: "  ")).font(.caption)
                    }
                }
                Section {
                    ForEach(posts) { p in
                        VStack(alignment: .leading) {
                            Text("@\(p.author ?? "unknown")").font(.caption).foregroundStyle(.secondary)
                            Text(p.body)
                            if let t = p.topics, !t.isEmpty {
                                Text(t.map { "#\($0)" }.joined(separator: " ")).font(.caption2)
                            }
                            Text("\(p.reply_count) replies · \(p.repost_count) reposts · \(p.quote_count) quotes").font(.caption2)
                            HStack {
                                Button("Reply") { replyTo = p.id; body_ = "" }
                                Button("Repost") {
                                    Task {
                                        do { try await client.createPulse(body: "", repostOf: p.id); load() }
                                        catch { self.error = errorMessage(error) }
                                    }
                                }
                                Button("Quote") {
                                    Task {
                                        do { try await client.createPulse(body: body_, quoteOf: p.id); body_ = ""; load() }
                                        catch { self.error = errorMessage(error) }
                                    }
                                }
                            }.font(.caption)
                        }
                    }
                }
            }
            .navigationTitle("Pulse")
            .onAppear(perform: load)
            .onChange(of: localOnly) { _ in load() }
        }
    }
}

// MARK: - Live Shopping

struct LiveShopView: View {
    @EnvironmentObject var session: SessionStore
    @State private var rooms: [ShopRoom] = []
    @State private var selected: ShopRoom?
    @State private var products: [ShopProduct] = []
    @State private var name = ""
    @State private var price = ""
    @State private var stock = ""
    @State private var coupon = ""
    @State private var notice: String?
    @State private var error: String?

    private var client: FeatureClient { FeatureClient(token: session.accessToken) }

    private func money(_ cents: Int) -> String { String(format: "$%d.%02d", cents / 100, cents % 100) }

    func loadProducts(_ roomId: String) {
        Task {
            do { products = try await client.liveProducts(roomId: roomId) }
            catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Rooms") {
                    ForEach(rooms) { r in
                        Button("\(r.title) (\(r.status))") {
                            selected = r; notice = nil; loadProducts(r.id)
                        }
                    }
                }
                Section("Add product") {
                    TextField("Name", text: $name)
                    TextField("Price (cents)", text: $price).keyboardType(.numberPad)
                    TextField("Stock", text: $stock).keyboardType(.numberPad)
                    Button("Create") {
                        guard let room = selected else { return }
                        Task {
                            do {
                                try await client.createProduct(roomId: room.id, name: name,
                                                               priceCents: Int(price) ?? 0, stock: Int(stock) ?? 0)
                                name = ""; price = ""; stock = ""
                                loadProducts(room.id)
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(selected == nil || name.isEmpty)
                }
                Section("Checkout") {
                    TextField("Coupon code", text: $coupon)
                }
                if let notice { Text(notice).foregroundStyle(.green).font(.caption) }
                Section("Products") {
                    ForEach(products) { p in
                        VStack(alignment: .leading) {
                            Text(p.name).font(.headline)
                            Text("\(money(p.price_cents)) · \(p.stock) in stock").font(.caption)
                            HStack {
                                Button("Pin") {
                                    guard let room = selected else { return }
                                    Task {
                                        do { try await client.pinProduct(roomId: room.id, productId: p.id); notice = "Pinned \(p.name)" }
                                        catch { self.error = errorMessage(error) }
                                    }
                                }
                                Button("Buy") {
                                    guard let room = selected else { return }
                                    Task {
                                        do {
                                            try await client.checkout(roomId: room.id, productId: p.id, coupon: coupon)
                                            notice = "Order placed for \(p.name)"
                                            loadProducts(room.id)
                                        } catch { self.error = errorMessage(error) }
                                    }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Live Shop")
            .task {
                do {
                    rooms = try await client.liveRooms()
                    if let first = rooms.first { selected = first; loadProducts(first.id) }
                } catch { self.error = errorMessage(error) }
            }
        }
    }
}

// MARK: - AI Studio

struct AiStudioView: View {
    @EnvironmentObject var session: SessionStore
    @State private var dubs: [DubJob] = []
    @State private var jobs: [ClipJob] = []
    @State private var mediaUrl = ""
    @State private var targetLang = "es"
    @State private var notice: String?
    @State private var error: String?

    private var client: FeatureClient { FeatureClient(token: session.accessToken) }

    func load() {
        Task {
            do { dubs = try await client.dubs() } catch { self.error = errorMessage(error) }
            do { jobs = try await client.clipJobs() } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Dub a video") {
                    TextField("Media URL", text: $mediaUrl)
                    TextField("Target language", text: $targetLang)
                    Button("Dub") {
                        Task {
                            do {
                                let r = try await client.dub(mediaUrl: mediaUrl, targetLang: targetLang)
                                notice = (r?.available ?? false)
                                    ? "Dub ready: \(r?.audio_url ?? "")"
                                    : "Dubbing unavailable: \(r?.reason ?? "no model configured")"
                                load()
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(mediaUrl.isEmpty)
                }
                Section("Generate clips") {
                    Button("Analyze") {
                        Task {
                            do {
                                let r = try await client.analyzeClips(mediaUrl: mediaUrl)
                                let n = r?.clips?.count ?? 0
                                notice = (r?.available ?? false)
                                    ? "Found \(n) candidate clip(s)"
                                    : "Clip generation unavailable: \(r?.reason ?? "no model configured")"
                                load()
                            } catch { self.error = errorMessage(error) }
                        }
                    }
                }
                if let notice { Text(notice).font(.caption).foregroundStyle(.blue) }
                Section("Dubs") {
                    ForEach(dubs) { d in
                        VStack(alignment: .leading) {
                            Text("→ \(d.target_lang ?? "?")").font(.subheadline)
                            if d.available, let url = d.audio_url, !url.isEmpty {
                                Text(url).font(.caption2)
                            } else {
                                Text("Unavailable\(d.reason.map { ": \($0)" } ?? "")").font(.caption2)
                            }
                        }
                    }
                }
                Section("Clips") {
                    ForEach(jobs) { j in
                        VStack(alignment: .leading) {
                            Text(j.clips?.first?.title ?? "(no clips)").font(.subheadline)
                            Text("score \(String(format: "%.2f", j.clips?.first?.score ?? 0)) · \((j.approved ?? false) ? "approved" : "pending approval")").font(.caption2)
                            Button("Approve") {
                                Task {
                                    do { try await client.approveClip(j.id, approve: true); notice = "Clip approved"; load() }
                                    catch { self.error = errorMessage(error) }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("AI Studio")
            .onAppear(perform: load)
        }
    }
}

// MARK: - Assistant

struct AssistantView: View {
    @EnvironmentObject var session: SessionStore
    @State private var conversations: [AssistantConversation] = []
    @State private var actions: [AssistantAction] = []
    @State private var active: String?
    @State private var turns: [(role: String, text: String)] = []
    @State private var input = ""
    @State private var error: String?

    private var client: FeatureClient { FeatureClient(token: session.accessToken) }

    func load() {
        Task {
            do {
                conversations = try await client.conversations()
                if active == nil { active = conversations.first?.id }
                actions = try await client.assistantActions()
            } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Conversations") {
                    Button("New") {
                        Task {
                            do { active = try await client.createConversation(title: "New conversation"); turns = []; load() }
                            catch { self.error = errorMessage(error) }
                        }
                    }
                    ForEach(conversations) { c in
                        Button(c.id.prefix(6) + "…") { active = c.id; turns = [] }
                    }
                }
                Section("Conversation") {
                    ForEach(Array(turns.enumerated()), id: \.offset) { _, t in
                        VStack(alignment: .leading) {
                            Text(t.role).font(.caption2).foregroundStyle(.secondary)
                            Text(t.text)
                        }
                    }
                }
                if !actions.isEmpty {
                    Section("Pending actions") {
                        ForEach(actions) { a in
                            VStack(alignment: .leading) {
                                Text("\(a.kind) · \(a.status)").font(.caption)
                                HStack {
                                    Button("Approve") {
                                        Task {
                                            do { try await client.decideAction(a.id, approve: true); load() }
                                            catch { self.error = errorMessage(error) }
                                        }
                                    }
                                    Button("Reject") {
                                        Task {
                                            do { try await client.decideAction(a.id, approve: false); load() }
                                            catch { self.error = errorMessage(error) }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                Section {
                    HStack {
                        TextField("Ask the assistant", text: $input)
                        Button("Send") {
                            guard let conv = active else { return }
                            let asked = input
                            turns.append((role: "user", text: asked))
                            input = ""
                            Task {
                                do {
                                    let r = try await client.ask(conversationId: conv, body: asked)
                                    let reply = (r?.reply?.isEmpty == false)
                                        ? r!.reply!
                                        : "Unavailable: \(r?.reason ?? "no assistant model configured")"
                                    turns.append((role: "assistant", text: reply))
                                    load()
                                } catch { self.error = errorMessage(error) }
                            }
                        }.disabled(input.isEmpty || active == nil)
                    }
                }
            }
            .navigationTitle("Assistant")
            .onAppear(perform: load)
        }
    }
}

// MARK: - Offline mesh status

struct MeshStatusView: View {
    @EnvironmentObject var session: SessionStore
    @State private var transport = "none"
    @State private var relayOk = true
    @State private var peers = 0
    @State private var pending = 0
    @State private var destination = ""
    @State private var message = ""
    @State private var notice: String?
    @State private var error: String?

    private var client: FeatureClient { FeatureClient(token: session.accessToken) }

    func refresh() {
        Task {
            do {
                let d = try await client.api.get("/api/mesh/status")
                if let o = try JSONSerialization.jsonObject(with: d) as? [String: Any] {
                    transport = (o["transport"] as? String) ?? (o["active_transport"] as? String) ?? "none"
                    relayOk = (o["relay_ok"] as? Bool) ?? true
                    peers = (o["peers"] as? [Any])?.count ?? (o["peers"] as? Int) ?? 0
                    pending = (o["pending"] as? [Any])?.count ?? (o["pending"] as? Int) ?? 0
                }
            } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Transport") {
                    Text("Active: \(transport)")
                    Text("Relay consent: \(relayOk ? "on" : "off")")
                    Button("Refresh", action: refresh)
                }
                Section("Send offline") {
                    TextField("Destination device id", text: $destination)
                    TextField("Message", text: $message)
                    Button("Queue") {
                        Task {
                            do {
                                let d = try await client.api.post("/api/mesh/send", body: [
                                    "dst": destination, "kind": "message", "body": message,
                                ])
                                if let o = try JSONSerialization.jsonObject(with: d) as? [String: Any] {
                                    notice = "Queued packet \((o["packet_id"] as? String) ?? "")"
                                }
                                message = ""; refresh()
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(destination.isEmpty || message.isEmpty)
                }
                Section("Queue") {
                    Text("\(peers) peers · \(pending) packet(s) awaiting a route")
                }
                if let notice { Text(notice).font(.caption).foregroundStyle(.blue) }
            }
            .navigationTitle("Offline Mesh")
            .onAppear(perform: refresh)
        }
    }
}
