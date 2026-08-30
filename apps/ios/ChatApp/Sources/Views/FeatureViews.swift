import SwiftUI
import AVKit

// Full parity with the web feature pages: For You, Groups (+events/RSVP),
// Pages, Monetization, Bots, Privacy. All views drive FeatureClient with the
// session token.

struct GroupsView: View {
    @EnvironmentObject var session: SessionStore
    @State private var client: FeatureClient?
    @State private var groups: [FeatureClient.Group] = []
    @State private var events: [String: [FeatureClient.Event]] = [:]
    @State private var joined: Set<String> = []
    @State private var expanded: Set<String> = []
    @State private var name = ""
    @State private var topic = ""
    @State private var isPrivate = false
    @State private var error: String?

    func load() {
        guard let client else { return }
        Task {
            do {
                groups = try await client.listGroups().groups
            } catch {
                error = errorMessage(error)
            }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Create group") {
                    TextField("Name", text: $name)
                    TextField("Description", text: $topic)
                    Toggle("Private", isOn: $isPrivate)
                    Button("Create") {
                        Task {
                            do {
                                try await client?.createGroup(name: name, description: topic, isPrivate: isPrivate)
                                name = ""; topic = ""; isPrivate = false
                                load()
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(name.isEmpty)
                }
                Section {
                    ForEach(groups, id: \.id) { g in
                        VStack(alignment: .leading) {
                            Text(g.name).font(.headline)
                            if let d = g.description { Text(d).font(.caption) }
                            HStack {
                                Button(joined.contains(g.id) ? "Leave" : "Join") {
                                    Task {
                                        do {
                                            if joined.contains(g.id) {
                                                try await client?.leaveGroup(g.id)
                                                joined.remove(g.id)
                                            } else {
                                                try await client?.joinGroup(g.id)
                                                joined.insert(g.id)
                                            }
                                        } catch { self.error = errorMessage(error) }
                                    }
                                }
                                Button("Events") {
                                    expanded.toggleMembership(g.id)
                                    if expanded.contains(g.id) {
                                        Task {
                                            do {
                                                let all = try await client?.listEvents().events ?? []
                                                events[g.id] = all.filter { $0.group_id == g.id }
                                            } catch { self.error = errorMessage(error) }
                                        }
                                    }
                                }
                            }
                            if expanded.contains(g.id) {
                                ForEach(events[g.id] ?? [], id: \.id) { ev in
                                    HStack {
                                        Text("\(ev.title) (\(ev.going_count) going)")
                                        Button("Going") {
                                            Task {
                                                try? await client?.rsvp(ev.id, response: "going")
                                                let all = (try? await client?.listEvents())?.events ?? []
                                                events[g.id] = all.filter { $0.group_id == g.id }
                                            }
                                        }
                                        Button("Interested") {
                                            Task {
                                                try? await client?.rsvp(ev.id, response: "interested")
                                                let all = (try? await client?.listEvents())?.events ?? []
                                                events[g.id] = all.filter { $0.group_id == g.id }
                                            }
                                        }
                                    }.font(.caption)
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Groups")
            .onAppear {
                client = FeatureClient(token: session.accessToken)
                load()
            }
        }
    }
}

struct PagesView: View {
    @EnvironmentObject var session: SessionStore
    @State private var client: FeatureClient?
    @State private var pages: [FeatureClient.PageInfo] = []
    @State private var name = ""
    @State private var category = ""
    @State private var following: Set<String> = []
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                Section("Create page") {
                    TextField("Name", text: $name)
                    TextField("Category", text: $category)
                    Button("Create") {
                        Task {
                            do {
                                try await client?.createPage(name: name, category: category, description: "")
                                name = ""; category = ""
                                pages = (try await client?.listPages())?.pages ?? []
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(name.isEmpty || category.isEmpty)
                }
                Section {
                    ForEach(pages, id: \.id) { p in
                        VStack(alignment: .leading) {
                            Text(p.name).font(.headline)
                            Text("\(p.category) \(p.follower_count) followers").font(.caption)
                            if let d = p.description { Text(d).font(.caption) }
                            Button(following.contains(p.id) ? "Unfollow" : "Follow") {
                                Task {
                                    do {
                                        if following.contains(p.id) {
                                            try await client?.unfollowPage(p.id)
                                            following.remove(p.id)
                                        } else {
                                            try await client?.followPage(p.id)
                                            following.insert(p.id)
                                        }
                                    } catch { self.error = errorMessage(error) }
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Pages")
            .onAppear {
                client = FeatureClient(token: session.accessToken)
                Task { pages = (try? await client?.listPages())?.pages ?? [] }
            }
        }
    }
}

struct MonetizeView: View {
    @EnvironmentObject var session: SessionStore
    @State private var client: FeatureClient?
    @State private var tiers: [FeatureClient.Tier] = []
    @State private var subs: [FeatureClient.Subscription] = []
    @State private var earnings: FeatureClient.Earnings?
    @State private var title = ""
    @State private var price = ""
    @State private var benefits = ""
    @State private var error: String?

    func load() {
        Task {
            do {
                tiers = (try await client?.myTiers())?.tiers ?? []
                subs = (try await client?.subscriptions())?.subscriptions ?? []
                earnings = try await client?.earnings()
            } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section("Create tier") {
                    TextField("Title", text: $title)
                    TextField("Price USD", text: $price)
                    TextField("Benefits", text: $benefits)
                    Button("Create") {
                        Task {
                            guard let amount = Double(price) else { return }
                            do {
                                try await client?.createTier(name: title, perks: benefits, priceUsd: amount)
                                title = ""; price = ""; benefits = ""
                                load()
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(title.isEmpty)
                }
                Section("My tiers") {
                    ForEach(tiers, id: \.id) { t in
                        Text("\(t.name) \u2014 $\(t.price_usd, specifier: "%.2f")/mo (\(t.subscriber_count) subs)")
                    }
                }
                Section("Memberships") {
                    ForEach(subs, id: \.id) { s in
                        HStack {
                            Text("\(s.tier_name) @\(s.creator_username) \(s.status)")
                            Button("Unsubscribe") {
                                Task { try? await client?.cancelSubscription(s.id); load() }
                            }
                        }
                    }
                }
                if let e = earnings {
                    Section("Earnings") {
                        Text("Earned $\(e.earned, specifier: "%.2f") \(e.currency)")
                        Text("Available $\(e.available, specifier: "%.2f") \u00b7 paid out $\(e.paid_out, specifier: "%.2f")")
                            .font(.caption)
                    }
                }
            }
            .navigationTitle("Monetization")
            .onAppear {
                client = FeatureClient(token: session.accessToken)
                load()
            }
        }
    }
}

struct BotsView: View {
    @EnvironmentObject var session: SessionStore
    @State private var client: FeatureClient?
    @State private var bots: [FeatureClient.Bot] = []
    @State private var username = ""
    @State private var displayName = ""
    @State private var description = ""
    @State private var newToken: String?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                if let token = newToken {
                    Section("Token (shown once)") { Text(token).font(.caption) }
                }
                Section("Create bot") {
                    TextField("Username", text: $username)
                    TextField("Display name", text: $displayName)
                    TextField("Description", text: $description)
                    Button("Create") {
                        Task {
                            do {
                                newToken = try await client?.createBot(
                                    username: username, displayName: displayName, description: description)
                                bots = (try await client?.myBots())?.bots ?? []
                            } catch { self.error = errorMessage(error) }
                        }
                    }.disabled(username.isEmpty)
                }
                Section {
                    ForEach(bots, id: \.id) { b in
                        HStack {
                            Text("@\(b.username)")
                            Text(b.active ? "active" : "inactive")
                            Button("Delete") {
                                Task { try? await client?.deleteBot(b.id)
                                    bots = (try? await client?.myBots())?.bots ?? [] }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Bots")
            .onAppear {
                client = FeatureClient(token: session.accessToken)
                Task { bots = (try? await client?.myBots())?.bots ?? [] }
            }
        }
    }
}

struct PrivacyView: View {
    @EnvironmentObject var session: SessionStore
    @State private var client: FeatureClient?
    @State private var mutes: [FeatureClient.Person] = []
    @State private var restricted: [FeatureClient.Person] = []
    @State private var filters: [FeatureClient.Filter] = []
    @State private var followReqs: [FeatureClient.FollowReq] = []
    @State private var locked = false
    @State private var phrase = ""
    @State private var error: String?

    func load() {
        Task {
            do {
                mutes = (try await client?.mutes())?.mutes ?? []
                restricted = (try await client?.restricted())?.restricted ?? []
                filters = (try await client?.filters())?.filters ?? []
                followReqs = (try await client?.followRequests())?.requests ?? []
            } catch { self.error = errorMessage(error) }
        }
    }

    var body: some View {
        NavigationStack {
            List {
                if let error { Text(error).foregroundColor(.red) }
                Section("Profile") {
                    Toggle("Lock profile", isOn: $locked)
                        .onChange(of: locked) { value in
                            Task { try? await client?.setProfileLock(value) }
                        }
                }
                Section("Follow requests") {
                    ForEach(followReqs, id: \.id) { r in
                        HStack {
                            Text("@\(r.username)")
                            Button("Accept") {
                                Task { try? await client?.acceptFollowRequest(r.id); load() }
                            }
                            Button("Decline") {
                                Task { try? await client?.declineFollowRequest(r.id); load() }
                            }
                        }
                    }
                }
                Section("Muted") {
                    ForEach(mutes, id: \.id) { u in
                        HStack { Text("@\(u.username)")
                            Button("Unmute") { Task { try? await client?.unmute(u.id); load() } } }
                    }
                }
                Section("Restricted") {
                    ForEach(restricted, id: \.id) { u in
                        HStack { Text("@\(u.username)")
                            Button("Remove") { Task { try? await client?.unrestrict(u.id); load() } } }
                    }
                }
                Section("Word filters") {
                    HStack {
                        TextField("Phrase", text: $phrase)
                        Button("Add") {
                            Task { try? await client?.addFilter(phrase); phrase = ""; load() }
                        }
                    }
                    ForEach(filters, id: \.phrase) { f in
                        HStack { Text(f.phrase)
                            Button("Remove") { Task { try? await client?.removeFilter(f.phrase); load() } } }
                    }
                }
            }
            .navigationTitle("Privacy")
            .onAppear {
                client = FeatureClient(token: session.accessToken)
                load()
            }
        }
    }
}

struct FypPost: Decodable, Identifiable {
    let id: String
    let body: String
    let username: String
    let media_url: String?
    let like_count: Int
}

struct FypView: View {
    @EnvironmentObject var session: SessionStore
    @State private var client: FeatureClient?
    @State private var posts: [FypPost] = []
    @State private var error: String?

    struct FypResponse: Decodable { let posts: [FypPost]? }

    var body: some View {
        NavigationStack {
            List(posts) { post in
                VStack(alignment: .leading) {
                    Text("@\(post.username)").font(.headline)
                    Text(post.body)
                    if let url = post.media_url, let u = URL(string: url) {
                        VideoPlayer(player: AVPlayer(url: u))
                            .frame(height: 220)
                    }
                    Text("\(post.like_count) likes").font(.caption)
                }
            }
            .navigationTitle("For You")
            .onAppear {
                client = FeatureClient(token: session.accessToken)
                Task {
                    do {
                        let data = try await client?.api.get("/api/fyp")
                        let resp = try JSONDecoder().decode(FypResponse.self, from: data ?? Data())
                        posts = resp.posts ?? []
                    } catch { error = errorMessage(error) }
                }
            }
        }
    }
}

private func errorMessage(_ error: Error) -> String {
    (error as? LocalizedError)?.errorDescription ?? error.localizedDescription
}

extension Set<String> {
    mutating func toggleMembership(_ value: String) {
        if contains(value) { remove(value) } else { insert(value) }
    }
}
